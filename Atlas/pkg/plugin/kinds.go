package plugin

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend/log"

	"github.com/flow-software/Timebase-Grafana/Atlas/pkg/atlas"
)

// Declared value kinds from the domain's ontology.
//
// A property's current value says nothing when it is empty (a TVQ with
// Value null, quality "No data"), so the kind shown in the picker and used
// to decide whether Trend can bucket a property comes from the type
// definitions first: GET .../entity-types. Live-checked on Atlas 2.0
// (cesmii-workshop): a live property is a TVQ compound whose Value type is
// set by compoundRange.fieldTypeOverrides.Value (xsd:double, xsd:string), or
// left open on the property and fixed per type by a propertyPin (atlas-common
// Tag.Data pinned to xsd:double on SqlTag and InfoHubKpi). A record (e.g.
// MaintainX LiveAsset) has its own compound type as range. A property left
// open has no declared kind; the current value decides, as before.

const tvqURI = "urn:atlas:ontology:atlas-common:TVQ"

// kindResolver answers the declared kind of a property on an entity type.
type kindResolver struct {
	types map[string]atlas.EntityType
	enums map[string]*atlas.EnumType // by URI
}

func newKindResolver(types []atlas.EntityType, enums []atlas.EnumType) *kindResolver {
	r := &kindResolver{types: make(map[string]atlas.EntityType, len(types)), enums: map[string]*atlas.EnumType{}}
	for _, t := range types {
		r.types[t.URI] = t
	}
	for i := range enums {
		r.enums[enums[i].URI] = &enums[i]
	}
	return r
}

// enumOf returns the enumeration type of property prop on an instance of
// typeURI (a TVQ's Value, or a plain property), or nil. A pin on the type
// wins over the property as attached to the type or an ancestor.
func (r *kindResolver) enumOf(typeURI, prop string) *atlas.EnumType {
	if r == nil || len(r.enums) == 0 {
		return nil
	}
	t, ok := r.types[typeURI]
	if !ok {
		return nil
	}
	for _, pin := range t.Pins {
		if localName(pin.PropertyURI) == prop {
			if e := r.enums[pin.FieldEnumOverrides["Value"]]; e != nil {
				return e
			}
		}
	}
	seen := map[string]bool{}
	for cur, ok := t, true; ok && !seen[cur.URI]; cur, ok = r.types[cur.ParentType] {
		seen[cur.URI] = true
		for _, p := range cur.Properties {
			if localName(p.URI) != prop {
				continue
			}
			if p.CompoundRange != nil {
				return r.enums[p.CompoundRange.FieldEnumOverrides["Value"]]
			}
			return r.enums[p.EnumType]
		}
	}
	return nil
}

// kind returns "number", "text", "boolean" or "record" for property prop
// (a local name, as on the instance) of an instance of typeURI, or "" when
// the ontology leaves it open or does not know the type.
func (r *kindResolver) kind(typeURI, prop string) string {
	if r == nil {
		return ""
	}
	t, ok := r.types[typeURI]
	if !ok {
		return ""
	}
	// Pins are effective on the type (own and inherited).
	for _, pin := range t.Pins {
		if localName(pin.PropertyURI) == prop {
			if k := xsdKind(pin.FieldTypeOverrides["Value"]); k != "" {
				return k
			}
		}
	}
	// The property as attached to the type or one of its ancestors.
	seen := map[string]bool{}
	for cur, ok := t, true; ok && !seen[cur.URI]; cur, ok = r.types[cur.ParentType] {
		seen[cur.URI] = true
		for _, p := range cur.Properties {
			if localName(p.URI) == prop {
				return propertyKind(p)
			}
		}
	}
	return ""
}

func propertyKind(p atlas.EntityProperty) string {
	cr := p.CompoundRange
	if cr == nil || cr.BaseCompoundURI == "" {
		return xsdKind(p.Range)
	}
	if cr.BaseCompoundURI != tvqURI {
		return "record"
	}
	return xsdKind(cr.FieldTypeOverrides["Value"])
}

// xsdKind maps an XML Schema datatype to a picker kind.
func xsdKind(uri string) string {
	if uri == "" {
		return ""
	}
	switch localName(uri) {
	case "double", "float", "decimal", "integer", "int", "long", "short", "byte",
		"nonNegativeInteger", "positiveInteger", "nonPositiveInteger", "negativeInteger",
		"unsignedLong", "unsignedInt", "unsignedShort", "unsignedByte":
		return "number"
	case "boolean":
		return "boolean"
	case "string", "normalizedString", "token", "dateTime", "date", "time", "duration", "anyURI":
		return "text"
	}
	return ""
}

// localName is the part after the last ':', '/' or '#'.
func localName(uri string) string {
	return uri[strings.LastIndexAny(uri, ":/#")+1:]
}

// kindCache keeps one resolver per branch/domain for treeTTL. A failed read
// (an older Atlas, or no permission) is cached as nil so the plugin falls
// back to current values without asking again on every query.
type kindCache struct {
	mu      sync.Mutex
	entries map[string]kindEntry
}

type kindEntry struct {
	r  *kindResolver
	at time.Time
}

func (d *Datasource) kinds(ctx context.Context, branch, domain string, refresh bool) *kindResolver {
	key := branch + "/" + domain
	c := &d.kindCache
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && !refresh && time.Since(e.at) < treeTTL {
		c.mu.Unlock()
		return e.r
	}
	c.mu.Unlock()

	var r *kindResolver
	types, err := d.client.EntityTypes(ctx, branch, domain)
	if err != nil {
		log.DefaultLogger.Warn("Atlas entity types unavailable; property kinds come from current values", "world", key, "error", err)
	} else {
		enums, eerr := d.client.EnumTypes(ctx, branch, domain)
		if eerr != nil {
			log.DefaultLogger.Info("Atlas enumeration types unavailable; enumerations show as their keys", "world", key, "error", eerr)
		}
		r = newKindResolver(types, enums)
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]kindEntry{}
	}
	c.entries[key] = kindEntry{r: r, at: time.Now()}
	c.mu.Unlock()
	return r
}
