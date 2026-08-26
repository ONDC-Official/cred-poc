# Credential type definitions

Git-tracked YAML definitions drive verification for each credential type.
Loaded once at process start from `CREDENTIAL_SERVICE_CREDENTIAL_TYPES_DIR`
(default: `configs/credential-types`).

Provider HTTP paths live in `configs/providers/` — credential types only
reference `provider.name` + `provider.capability`.

## Schema

| Field | Purpose |
|---|---|
| `credential_type` | Registry key (`PAN`, `GST`, …) |
| `version` | Major definition version |
| `issuer` | Enum value in `CRED_ISSUER` (e.g. `INCOME_TAX_DEPT`) |
| `provider.name` | Provider key from `configs/providers/` (e.g. `digio`) |
| `provider.capability` | Named capability from that provider’s catalog |
| `normalization` | `trim` / `uppercase` / `remove_chars` applied to `id_no` |
| `validation.fields.<name>` | Per–cred_data-field rules (see below). Omitted fields are not validated |
| `request.fields` | Provider body key → source (`normalized_id`, or any cred_data key e.g. `name`, `dob`) |
| `request.transformer` | Optional named request transformer |
| `response.transformer` | Optional named response transformer |
| `response.success_field` | Declarative CredID source (when no response transformer) |
| `response.soft_fail_field` | Non-empty provider field ⇒ soft failure |
| `response.fields` | Provider key → `VerifiedData` key (declarative parse) |

### Field validation

Under `validation.fields`, each key is a `cred_data` field name. Rules for that field only:

| Sub-field | Purpose |
|---|---|
| `required` | Non-empty after normalization (`id_no`) or trim (other fields) |
| `patterns` | OR-matched regexes (also tried after hyphen/space strip). Empty optional fields skip pattern checks |
| `message` | Optional override for the format-error text |

Example — validate `id_no` today, and optionally `name` later without code changes:

```yaml
validation:
  fields:
    id_no:
      required: true
      patterns:
        - "^[A-Z]{5}[0-9]{4}[A-Z]$"
      message: "invalid PAN format"
    # name:
    #   required: true
    #   patterns:
    #     - "^[A-Za-z .'-]+$"
```

No secrets, scripts, or vendor base URLs in YAML. Unknown transformer names or
unknown `provider.name` / `provider.capability` pairs fail boot.
