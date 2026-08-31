# Credential type definitions

Git-tracked YAML definitions drive verification for each credential type.
Loaded once at process start from `CREDENTIAL_SERVICE_CREDENTIAL_TYPES_DIR`
(default: `configs/credential-types`).

Provider HTTP paths live in `configs/providers/` — credential types only
reference `providers[].name` + `providers[].capability`.

## Schema

| Field | Purpose |
|---|---|
| `credential_type` | Registry key (`PAN`, `GST`, …) |
| `version` | Major definition version |
| `issuer` | Enum value in `CRED_ISSUER` (e.g. `INCOME_TAX_DEPT`) |
| `providers` | Ordered list of `{name, capability}`. Tried in order — a transport error or a failed result falls through to the next entry; the first success wins |
| `normalization` | `trim` / `uppercase` / `remove_chars` applied to `id_no` |
| `validation.fields.<name>` | Per–cred_data-field rules (see below). Omitted fields are not validated |
| `request.fields` | Provider body key → source (`normalized_id`, or any cred_data key e.g. `name`, `dob`) |
| `request.transformer` | Optional named request transformer |
| `response.transformer` | Optional named response transformer |
| `response.success_field` | Declarative CredID source (when no response transformer) |
| `response.soft_fail_field` | Non-empty provider field ⇒ soft failure |
| `response.fields` | Provider key → `VerifiedData` key (declarative parse) |

### Field validation

Under `validation.fields`, each key is a `cred_data` field name. This is a
presence check only — no format/regex validation is performed:

| Sub-field | Purpose |
|---|---|
| `required` | Non-empty after normalization (`id_no`) or trim (other fields) |

Example — require `id_no` today, and optionally `name` later without code changes:

```yaml
validation:
  fields:
    id_no:
      required: true
    # name:
    #   required: true
```

### Multi-provider fallback

```yaml
providers:
  - name: digio
    capability: fetch_id_data_pan
  - name: mock
    capability: fetch_id_data_pan
```

Providers are tried strictly in list order. Every listed provider needs a
matching capability in its `configs/providers/<name>.v1.yaml` catalog and a
registered runtime `Caller` (wired in `internal/bootstrap/clients.go`) —
this is validated at boot. `providers` must not be empty.

No secrets, scripts, or vendor base URLs in YAML. Unknown transformer names or
unknown `providers[].name` / `providers[].capability` pairs fail boot.
