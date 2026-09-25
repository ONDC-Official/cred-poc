# ONDC Credential Service

A Go microservice that verifies business credentials (**PAN**, **GST**, **FSSAI**, **UDYAM**) for the
ONDC (Open Network for Digital Commerce) network through a KYC provider.

## Features

- **Credential Verification**: PAN, GST, FSSAI and UDYAM in a single API call
- **ONDC Signature Auth**: Ed25519 + BLAKE2b-512 request signing, keys resolved from the ONDC Registry
- **Config-Driven Types**: Credential types and providers defined in YAML
- **Provider Fallback**: Providers tried in order; the first success wins


## POST /verify

Checks whether a business credential (**PAN**, **GST**, **FSSAI** or **UDYAM**) is valid and returns
the result in the same call.

---

## Endpoint

```
POST https://<credential-service-host>/verify
```

| Header | Value |
|---|---|
| `Content-Type` | `application/json` |
| `Authorization` | ONDC signature of the request body (see [Authorization](#authorization)) |

---

## Request payload

**Required fields only:**

```json
{
  "cred_type": "GST",
  "cred_id": "29AABCU9603R1ZM"
}
```

**With optional fields:**

```json
{
  "cred_type": "PAN",
  "cred_id": "ABCDE1234F",
  "name": "Ravi Kumar",
  "dob": "01/01/1990"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `cred_type` | string | yes | `PAN`, `GST`, `FSSAI` or `UDYAM`, in uppercase |
| `cred_id` | string | yes | The credential number to verify |
| `name` | string | no | Name of the credential holder. Accepted, but not used for verification at present. |
| `dob` | string | no | Date of birth of the credential holder. Accepted, but not used for verification at present. |

| `cred_type` | `cred_id` example |
|---|---|
| `PAN` | `ABCDE1234F` |
| `GST` | `29AABCU9603R1ZM` |
| `FSSAI` | `21523064000396` |
| `UDYAM` | `UDYAM-MH-01-1234567` |

### Optional query parameter

| Parameter | Type | Required | Description |
|---|---|---|---|
| `verbose` | boolean | no | `POST /verify?verbose=true` adds `provider_response`, the verification source's raw reply, to the response |

---

## Response

### Verified

```json
{
  "success": true,
  "provider": "digio",
  "cred_id": "29AABCU9603R1ZM",
  "data": {
    "gstin": "29AABCU9603R1ZM",
    "corporate_name": "EXAMPLE TRADERS PRIVATE LIMITED",
    "details": {}
  }
}
```

### Not verified

```json
{
  "success": false,
  "provider": "digio",
  "error": "Invalid GSTIN / UID"
}
```

| Field | Description |
|---|---|
| `success` | `true` if the credential is verified, `false` if not |
| `provider` | The verification source that answered |
| `cred_id` | The verified credential number (only when `success` is `true`) |
| `data` | Details of the credential (only when `success` is `true`) |
| `error` | Reason for failure (only when `success` is `false`) |
| `provider_response` | Raw reply from the verification source (only with `?verbose=true`) |

### `data` by credential type

| `cred_type` | Fields in `data` |
|---|---|
| `PAN` | `pan`, `full_name`, `category`, `status` |
| `GST` | `gstin`, `corporate_name`, `details` |
| `FSSAI` | `license_no`, `company_name`, `premises_address`, `status`, and either `state`, `license_status`, `license_category` or `expiry_date`, `kind_of_business` |
| `UDYAM` | `uan`, `name_of_enterprise`, `enterprise_type`, `classification_date`, `major_activity`, `nic_2_digit` |

Any field in `data` can be empty if the source did not return it.

### Status codes

| HTTP | Meaning | Example `error` |
|---|---|---|
| `200` | Verification ran. Check `success`. | — |
| `400` | Invalid request | `cred_id is required`, `cred_type is required`, `unsupported credential type: pan`, `invalid JSON` |
| `401` | Authorization failed | `authorization header is required`, `invalid authorization header: invalid signature`, `invalid authorization header: authorization header expired` |
| `502` | The verification source returned an unreadable reply; try again later | — |

Every non-`200` body looks like `{"error": "..."}`.

---

## How to use the result

- **Verified:** HTTP `200` with `success: true`.
- **Not verified:** HTTP `200` with `success: false`. Show the seller the `error`, or ask them to
  recheck the number.
- **Try again later** if the `error` mentions `status 5xx`, or on `502` or a timeout.
- **Fix the request** on `400` / `401`. Retrying it unchanged will fail again.
- One request verifies one credential. Allow a timeout of up to **90 seconds**.

---

## Authorization

Sign every request with your ONDC signing key, the same scheme as other ONDC calls. Your
`subscriber_id` and `unique_key_id` must be `SUBSCRIBED` in the ONDC Registry.

1. Serialize the body to a string, once.
2. `digest` = base64( BLAKE2b-512( body ) )
3. Signing string (three lines joined by `\n`):
   ```
   (created): <unix-time-now>
   (expires): <unix-time-now + 300>
   digest: BLAKE-512=<digest>
   ```
4. `signature` = base64( Ed25519-sign( signing string, private key ) )
5. Header, all on one line:
   ```
   Signature keyId="<subscriber_id>|<unique_key_id>|ed25519",algorithm="ed25519",created="<created>",expires="<expires>",headers="(created) (expires) digest",signature="<signature>"
   ```

**Send exactly the string you signed.** If your HTTP client re-serializes the JSON, the signature
fails with `401`.

---

## Example (Node.js 18+)

```js
const crypto = require("node:crypto");

const BASE_URL = process.env.CREDENTIAL_SERVICE_URL;           // https://<credential-service-host>
const SUBSCRIBER_ID = process.env.ONDC_SUBSCRIBER_ID;
const UNIQUE_KEY_ID = process.env.ONDC_UNIQUE_KEY_ID;
const PRIVATE_KEY_B64 = process.env.ONDC_SIGNING_PRIVATE_KEY;  // base64 Ed25519 private key

function authorizationHeader(body) {
  const created = Math.floor(Date.now() / 1000);
  const expires = created + 300;
  const digest = crypto.createHash("blake2b512").update(body, "utf8").digest("base64");
  const signingString = `(created): ${created}\n(expires): ${expires}\ndigest: BLAKE-512=${digest}`;

  const seed = Buffer.from(PRIVATE_KEY_B64, "base64").subarray(0, 32);
  const key = crypto.createPrivateKey({
    key: Buffer.concat([Buffer.from("302e020100300506032b657004220420", "hex"), seed]),
    format: "der",
    type: "pkcs8",
  });
  const signature = crypto.sign(null, Buffer.from(signingString, "utf8"), key).toString("base64");

  return `Signature keyId="${SUBSCRIBER_ID}|${UNIQUE_KEY_ID}|ed25519",algorithm="ed25519",` +
    `created="${created}",expires="${expires}",headers="(created) (expires) digest",signature="${signature}"`;
}

async function verifyIdentity(credType, credId) {
  const body = JSON.stringify({ cred_type: credType, cred_id: credId });
  const res = await fetch(`${BASE_URL}/verify`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Authorization: authorizationHeader(body) },
    body,
    signal: AbortSignal.timeout(90_000),
  });
  const result = await res.json();
  const verified = res.status === 200 && result.success === true;
  return { status: res.status, verified, result };
}

verifyIdentity("GST", "29AABCU9603R1ZM").then(console.log);
```

<details>
<summary>Python example (<code>pip install pynacl requests</code>)</summary>

```python
import base64, hashlib, json, os, time
import nacl.signing, requests

BASE_URL = os.environ["CREDENTIAL_SERVICE_URL"]
SUBSCRIBER_ID = os.environ["ONDC_SUBSCRIBER_ID"]
UNIQUE_KEY_ID = os.environ["ONDC_UNIQUE_KEY_ID"]
PRIVATE_KEY_B64 = os.environ["ONDC_SIGNING_PRIVATE_KEY"]


def authorization_header(body: str) -> str:
    created = int(time.time())
    expires = created + 300
    digest = base64.b64encode(hashlib.blake2b(body.encode(), digest_size=64).digest()).decode()
    signing_string = f"(created): {created}\n(expires): {expires}\ndigest: BLAKE-512={digest}"
    seed = base64.b64decode(PRIVATE_KEY_B64)[:32]
    signature = base64.b64encode(nacl.signing.SigningKey(seed).sign(signing_string.encode()).signature).decode()
    return (f'Signature keyId="{SUBSCRIBER_ID}|{UNIQUE_KEY_ID}|ed25519",algorithm="ed25519",'
            f'created="{created}",expires="{expires}",headers="(created) (expires) digest",signature="{signature}"')


def verify_identity(cred_type: str, cred_id: str):
    body = json.dumps({"cred_type": cred_type, "cred_id": cred_id}, separators=(",", ":"))
    res = requests.post(f"{BASE_URL}/verify", data=body.encode(), timeout=90,
                        headers={"Content-Type": "application/json", "Authorization": authorization_header(body)})
    result = res.json()
    verified = res.status_code == 200 and result.get("success") is True
    return res.status_code, verified, result


print(verify_identity("GST", "29AABCU9603R1ZM"))
```

</details>
