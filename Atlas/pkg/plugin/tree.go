package plugin

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
)

// TreeNode is one instance in the editor's browse tree. ParentID is the
// instance that contains it, by the same rule as the Atlas Instances view:
// the source of a relationship whose type is a containment type.
type TreeNode struct {
	ID       string `json:"id"`
	Label    string `json:"label,omitempty"`
	Type     string `json:"type"`
	ParentID string `json:"parentId,omitempty"`
	// Live (timestamp/value/quality) properties, for picking History series.
	Live []LiveProperty `json:"live,omitempty"`
}

// LiveProperty is a property with stored history and the kind of its value.
type LiveProperty struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// instanceTree builds the containment tree for one world in four reads:
// the instance list, node labels (Cypher, because /instances has no label),
// relationship types (for isContainment) and all edges.
func (d *Datasource) instanceTree(ctx context.Context, branch, domain string) ([]TreeNode, error) {
	items, _, err := d.client.Instances(ctx, branch, domain, "", resourceListLimit)
	if err != nil {
		return nil, err
	}
	relTypes, err := d.client.RelationshipTypes(ctx, branch, domain)
	if err != nil {
		return nil, err
	}
	containment := map[string]bool{}
	for _, rt := range relTypes {
		if rt.IsContainment {
			containment[rt.URI] = true
		}
	}

	labels := map[string]string{}
	live := map[string][]LiveProperty{}
	declared := d.kinds(ctx, branch, domain, false)
	res, err := d.client.Query(ctx, branch, domain, "MATCH (n) RETURN n", nil)
	if err != nil {
		return nil, err
	}
	rows, err := decodeRows(res)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if len(row) > 0 && isNode(row[0]) {
			m := row[0].(map[string]any)
			id := str(m["entityId"])
			if l := str(m["entityLabel"]); l != "" {
				labels[id] = l
			}
			for _, p := range liveProperties([]map[string]any{m}) {
				kind := p.Kind
				// The ontology knows the type even when the current value is empty.
				if k := declared.kind(str(m["typeId"]), p.Name); k != "" {
					kind = k
				}
				live[id] = append(live[id], LiveProperty{Name: p.Name, Kind: kind})
			}
		}
	}

	// Outgoing containment edges per source.
	children := map[string][]string{}
	if len(containment) > 0 {
		res, err := d.client.Query(ctx, branch, domain, "MATCH (a)-[r]->(b) RETURN r.Source, r.Target, type(r)", nil)
		if err != nil {
			return nil, err
		}
		rows, err := decodeRows(res)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			if len(row) < 3 || !containment[str(row[2])] {
				continue
			}
			src, dst := str(row[0]), str(row[1])
			children[src] = append(children[src], dst)
		}
	}

	// Like Atlas: a child belongs to the first containing source in instance
	// order, so a containment DAG still renders as a tree.
	known := make(map[string]bool, len(items))
	for _, it := range items {
		known[it.ElementID] = true
	}
	parent := map[string]string{}
	for _, it := range items {
		for _, c := range children[it.ElementID] {
			if c != it.ElementID && known[c] && parent[c] == "" {
				parent[c] = it.ElementID
			}
		}
	}
	breakCycles(parent)

	out := make([]TreeNode, len(items))
	for i, it := range items {
		out[i] = TreeNode{ID: it.ElementID, Label: labels[it.ElementID], Type: shortName(it.TypeURI), ParentID: parent[it.ElementID], Live: live[it.ElementID]}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(nodeName(out[i])) < strings.ToLower(nodeName(out[j])) })
	return out, nil
}

func nodeName(n TreeNode) string {
	if n.Label != "" {
		return n.Label
	}
	return n.ID
}

// breakCycles drops a parent link that would make an instance its own
// ancestor. Atlas validates containment as acyclic; this is a safety net so
// the editor never loops or hides an instance.
func breakCycles(parent map[string]string) {
	for id := range parent {
		seen := map[string]bool{id: true}
		for p := parent[id]; p != ""; p = parent[p] {
			if seen[p] {
				delete(parent, id)
				break
			}
			seen[p] = true
		}
	}
}

// treeCache keeps each world's tree for a minute, so reopening the browser
// or switching panels does not reread the whole model.
type treeCache struct {
	mu      sync.Mutex
	entries map[string]treeEntry
}

type treeEntry struct {
	at    time.Time
	nodes []TreeNode
}

const treeTTL = time.Minute

func (c *treeCache) get(key string, refresh bool, load func() ([]TreeNode, error)) ([]TreeNode, error) {
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && !refresh && time.Since(e.at) < treeTTL {
		c.mu.Unlock()
		return e.nodes, nil
	}
	c.mu.Unlock()
	nodes, err := load()
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]treeEntry{}
	}
	c.entries[key] = treeEntry{at: time.Now(), nodes: nodes}
	c.mu.Unlock()
	return nodes, nil
}
