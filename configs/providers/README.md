# Provider definitions

Git-tracked YAML catalogs describe each KYC **provider** and its named **capabilities**.
Loaded once at process start from `CREDENTIAL_SERVICE_CREDENTIAL_PROVIDERS_DIR`
(default: `configs/providers`).

Credential-type configs (`configs/credential-types/`) reference
`provider.name` + `provider.capability` only — they never embed vendor URLs.
Paths, HTTP methods, and capability catalogs live here so a new provider can be
added without rewriting credential-type logic.

## Schema

| Field | Purpose |
|---|---|
| `name` | Provider key (`digio`, future `signzy`, …). Must match a registered runtime client |
| `version` | Definition version |
| `capabilities.<id>` | Named operation referenced by credential-type YAML |
| `capabilities.<id>.method` | HTTP method (`POST` today) |
| `capabilities.<id>.path` | Path only (no scheme/host); base URL comes from env/secrets |

## Digio

Runtime client secrets:

| Env | Purpose |
|---|---|
| `CREDENTIAL_SERVICE_DIGIO_BASE_URL` | Digio API base URL |
| `CREDENTIAL_SERVICE_DIGIO_TOKEN` | Basic auth token |
| `CREDENTIAL_SERVICE_DIGIO_TIMEOUT` | HTTP timeout (default `30s`) |

## Adding a provider later

1. Add `configs/providers/<name>.v1.yaml` with that vendor’s capabilities.
2. Implement a `provider.Caller` for the vendor and register it at bootstrap under the same `name`.
3. Point credential-type YAML at `provider.name` / `provider.capability`.

No secrets or full base URLs in YAML.
