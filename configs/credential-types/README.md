# Credential type definitions

Git-tracked YAML definitions drive Digio verification for each credential type.
Loaded once at process start from `CREDENTIAL_SERVICE_CREDENTIAL_TYPES_DIR`
(default: `configs/credential-types`).

## Schema

| Field | Purpose |
|---|---|
| `credential_type` | Registry key (`PAN`, `GST`, …) |
| `version` | Major definition version |
| `issuer` | Enum value in `CRED_ISSUER` (e.g. `INCOME_TAX_DEPT`) |
| `provider.name` | Must be `digio` for now |
| `provider.endpoint` | Digio path only; base URL stays in env |
| `normalization` | `trim` / `uppercase` / `remove_chars` applied to `id_no` |
| `validation.patterns` | OR-matched regexes (also tried after hyphen/space strip) |
| `validation.required_fields` | Extra cred_data keys required (e.g. PAN `name`, `dob`) |
| `request.fields` | Digio body key → source (`normalized_id`, `name`, `dob`, …) |
| `request.transformer` | Optional named request transformer |
| `response.transformer` | Optional named response transformer |
| `response.success_field` | Declarative CredID source (when no response transformer) |
| `response.soft_fail_field` | Non-empty Digio field ⇒ soft failure |
| `response.fields` | Digio key → `VerifiedData` key (declarative parse) |

No secrets, scripts, or full Digio base URLs in YAML. Unknown transformer names fail boot.
