import React, { FormEvent } from 'react';
import { DataSourcePluginOptionsEditorProps, SelectableValue } from '@grafana/data';
import { Field, FieldSet, Input, RadioButtonGroup, SecretInput, SecretTextArea, Switch } from '@grafana/ui';

import { AtlasOptions, AtlasSecureJsonData, AuthType } from '../types';

interface Props extends DataSourcePluginOptionsEditorProps<AtlasOptions, AtlasSecureJsonData> {}

const authOptions: Array<SelectableValue<AuthType>> = [
  { label: 'Client credentials', value: 'clientCredentials', description: 'Fetch and renew tokens automatically' },
  { label: 'Bearer token', value: 'bearerToken', description: 'A fixed token; not renewed when it expires' },
  { label: 'None', value: 'none' },
];

export function ConfigEditor({ options, onOptionsChange }: Props) {
  const { jsonData, secureJsonFields } = options;
  const secureJsonData = options.secureJsonData ?? {};
  const authType = jsonData.authType ?? 'clientCredentials';

  const setJson = <K extends keyof AtlasOptions>(key: K, value: AtlasOptions[K]) =>
    onOptionsChange({ ...options, jsonData: { ...jsonData, [key]: value } });

  const setSecret = (key: keyof AtlasSecureJsonData) => (e: FormEvent<HTMLInputElement | HTMLTextAreaElement>) =>
    onOptionsChange({ ...options, secureJsonData: { ...secureJsonData, [key]: e.currentTarget.value } });

  const resetSecret = (key: keyof AtlasSecureJsonData) => () =>
    onOptionsChange({
      ...options,
      secureJsonFields: { ...secureJsonFields, [key]: false },
      secureJsonData: { ...secureJsonData, [key]: '' },
    });

  return (
    <>
      <FieldSet label="Connection">
        <Field label="URL" description="The Timebase Atlas address on its HTTPS port, without a path." required>
          <Input
            id="config-url"
            width={60}
            value={options.url}
            placeholder="https://atlas:4552"
            onChange={(e) => onOptionsChange({ ...options, url: e.currentTarget.value })}
          />
        </Field>
        <Field label="Default branch" description="Used by queries that do not pick a branch. Usually main.">
          <Input
            id="config-default-branch"
            width={30}
            value={jsonData.defaultBranch ?? ''}
            placeholder="main"
            onChange={(e) => setJson('defaultBranch', e.currentTarget.value)}
          />
        </Field>
        <Field
          label="Default domain"
          description="Optional. Used by queries that do not pick a domain. Save & test lists the compiled domains."
        >
          <Input
            id="config-default-domain"
            width={30}
            value={jsonData.defaultDomain ?? ''}
            onChange={(e) => setJson('defaultDomain', e.currentTarget.value)}
          />
        </Field>
      </FieldSet>

      <FieldSet label="Authentication">
        <Field label="Method">
          <RadioButtonGroup options={authOptions} value={authType} onChange={(v) => setJson('authType', v)} />
        </Field>

        {authType === 'clientCredentials' && (
          <>
            <Field
              label="Token URL"
              description="OAuth 2.0 token endpoint. For Timebase this is Pulse's /auth/token."
              required
            >
              <Input
                id="config-token-url"
                width={60}
                value={jsonData.tokenUrl ?? ''}
                placeholder="https://pulse:4542/auth/token"
                onChange={(e) => setJson('tokenUrl', e.currentTarget.value)}
              />
            </Field>
            <Field label="Client ID" required>
              <Input
                id="config-client-id"
                width={40}
                value={jsonData.clientId ?? ''}
                placeholder="Atlas"
                onChange={(e) => setJson('clientId', e.currentTarget.value)}
              />
            </Field>
            <Field label="Client secret" required>
              <SecretInput
                id="config-client-secret"
                width={40}
                isConfigured={!!secureJsonFields.clientSecret}
                value={secureJsonData.clientSecret ?? ''}
                onChange={setSecret('clientSecret')}
                onReset={resetSecret('clientSecret')}
              />
            </Field>
            <Field label="Scope" description="Optional. Space-separated scopes to request.">
              <Input
                id="config-scope"
                width={40}
                value={jsonData.scope ?? ''}
                onChange={(e) => setJson('scope', e.currentTarget.value)}
              />
            </Field>
          </>
        )}

        {authType === 'bearerToken' && (
          <Field label="Token" description="Paste the token only. Queries stop working when it expires." required>
            <SecretInput
              id="config-bearer-token"
              width={60}
              isConfigured={!!secureJsonFields.bearerToken}
              value={secureJsonData.bearerToken ?? ''}
              onChange={setSecret('bearerToken')}
              onReset={resetSecret('bearerToken')}
            />
          </Field>
        )}
      </FieldSet>

      <FieldSet label="TLS">
        <Field
          label="Skip certificate verification"
          description="Accept self-signed certificates. Applies to both the token URL and Atlas. Avoid in production."
        >
          <Switch
            id="config-tls-skip-verify"
            value={!!jsonData.tlsSkipVerify}
            onChange={(e) => setJson('tlsSkipVerify', e.currentTarget.checked)}
          />
        </Field>
        <Field label="Custom CA certificate" description="Trust servers signed by this CA, e.g. the Pulse root CA.">
          <Switch
            id="config-tls-ca"
            value={!!jsonData.tlsAuthWithCACert}
            onChange={(e) => setJson('tlsAuthWithCACert', e.currentTarget.checked)}
          />
        </Field>
        {jsonData.tlsAuthWithCACert && (
          <Field label="CA certificate (PEM)">
            <SecretTextArea
              id="config-tls-ca-cert"
              cols={60}
              rows={6}
              isConfigured={!!secureJsonFields.tlsCACert}
              value={secureJsonData.tlsCACert ?? ''}
              placeholder="-----BEGIN CERTIFICATE-----"
              onChange={setSecret('tlsCACert')}
              onReset={resetSecret('tlsCACert')}
            />
          </Field>
        )}
      </FieldSet>
    </>
  );
}
