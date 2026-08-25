"""
ONDC TEAM Scheme — Master Pipeline v4
======================================
1. Reads seller CSV (Input File Format) + on_search JSONs
2. Updates master seller database (Seller Credential Format) incrementally
3. Verifies PAN, FSSAI, Udyam, GST via Digio API (retry up to 5x)
4. Maps Digio responses to master DB columns (confirmed mapping)
5. Generates credential report PDFs, uploads to Drive
6. Scores catalog from on_search JSON + category mapping Excel
7. Generates catalog report PDFs, uploads to Drive

Env vars:
    DIGIO_AUTHORIZATION=Basic QUk2OUlM...    (pre-encoded)
    — OR —
    DIGIO_CLIENT_ID=xxx + DIGIO_CLIENT_SECRET=yyy
    DIGIO_BASE_URL=https://api.digio.in       (default)

Usage:  python team_pipeline.py
"""

import os, sys, re, json, math, base64, time
from datetime import datetime
from pathlib import Path
from copy import deepcopy

import pandas as pd
import requests
from reportlab.lib.pagesizes import A4
from reportlab.lib.styles import getSampleStyleSheet, ParagraphStyle
from reportlab.lib.units import cm
from reportlab.lib import colors
from reportlab.platypus import (
    SimpleDocTemplate, Paragraph, Spacer, Table, TableStyle, Image
)
from reportlab.lib.enums import TA_LEFT

try:
    from drive_uploader import DriveUploader
    DRIVE_OK = True
except ImportError:
    DRIVE_OK = False
    print("NOTE: drive_uploader.py not found in script folder\n")

try:
    from supabase_uploader import upload_batch as supabase_upload
    SUPABASE_OK = True
except ImportError:
    SUPABASE_OK = False

# ═══════════════════════════════════════════════════════════════
# CONFIG
# ═══════════════════════════════════════════════════════════════
SD = Path(__file__).parent.resolve()
DIGIO_AUTH   = os.environ.get("DIGIO_AUTHORIZATION", "").strip()
DIGIO_ID     = os.environ.get("DIGIO_CLIENT_ID", "").strip()
DIGIO_SECRET = os.environ.get("DIGIO_CLIENT_SECRET", "").strip()
DIGIO_URL    = os.environ.get("DIGIO_BASE_URL", "https://api.digio.in").rstrip("/")
DEFAULT_DOB  = "01/01/2020"
MAX_RETRIES  = 5

# Drive folders (credential and catalog report destinations)
DRIVE_CRED     = "1MP2TONuxb2v5h0967UTLQQBDcUswaQhd"
DRIVE_CAT      = "16x5FUiFtad1Inu_A2kgbAJ6rE7DOP_Fl"
MAPPINGS = SD / "mappings"
LOGO = "ondc_logo.png"

# Master DB path (update this to your preferred location)
MASTER_DB_PATH = SD / "master_seller_database.xlsx"

# Domain config
# ── Domain codes follow the ONDC Category Taxonomy v1.2 contract ──
#    RET10 Grocery | RET11 F&B | RET12 Fashion | RET13 BPC |
#    RET14 Electronics | RET15 Appliances | RET16 Home & Kitchen |
#    RET18 Health & Wellness
_DC = {
    "RET10":{"l":"Grocery","m":"Grocery_mapping.xlsx","s":"_Grocery"},
    "RET11":{"l":"F&B","m":"FB_Category_Mapping.xlsx","s":""},
    "RET12":{"l":"Fashion","m":"Fashion_Catalog_Mapping.xlsx","s":"_Fashion"},
    "RET13":{"l":"BPC","m":"BPC_Catalog_Mapping.xlsx","s":"_BPC"},
    "RET14":{"l":"Electronics","m":"Electronics_Catalog_Mapping.xlsx","s":"_Electronics"},
    "RET15":{"l":"Appliances","m":"Appliances_mapping.xlsx","s":"_Appliances"},
    "RET16":{"l":"Home & Kitchen","m":"HK_Catalog_Mapping.xlsx","s":"_HK"},
    "RET18":{"l":"Health & Wellness","m":"Health___Wellness_Mapping.xlsx","s":"_Health"},
}

def dc(d):
    m = re.search(r'(RET\d+)', str(d).upper())
    return _DC.get(m.group(1)) if m else None

def dl(d):
    c = dc(d)
    return c["l"] if c else str(d).strip()

# ═══════════════════════════════════════════════════════════════
# MASTER DB COLUMNS (Seller Credential Format)
# ═══════════════════════════════════════════════════════════════
MASTER_COLS = [
    "Provider ID","Bpp ID","Sellerapp","Domain","Provider Name",
    "TEAM ID","Seller Pan No","Server Date","Pickup Gps","Pickup Locality",
    "Pickup Street","Pickup City","Pickup Pincode","Pickup State",
    "Provider Contact No","Provider Email",
    "Sellerapp Bank No","Sellerapp Bank Ifsc Code",
    "FSSAI.id","FSSAI.validationstatus","FSSAI.requeststatus",
    "FSSAI.premiseaddress","FSSAI.state","fssai.pincode",
    "FSSAI.fullpremiseaddress","FSSAI.licensestatus","FSSAI.companyname",
    "Fssai.applicationtype","fssai.licensecategory","fssai.error",
    "FSSAI.timestamp","FSSAI.verificationproof","FSSAI.expiry",
    "PAN.id","PAN.status","PAN.timestamp","PAN.verificationproof",
    "GST","gst.status","gst.checkstatus","gst.legalname",
    "gst.duty","gst.enterprisetype","gst.error",
    "gst.registrationdate","gst.centraltaxjurisdiction",
    "gst.address.flatnumber","gst.address.buildingnumber",
    "gst.address.buildingname","gst.address.buildingname.1",
    "gst.address.area","gst.address.city","gst.address.state",
    "gst.address.pincode","gst.timestamp","gst.verificationproof",
    "udyam.id","udyam.status","udyam.enterprisetype",
    "udyam.classificationdate","udyam.enterprisename",
    "udyam.majoractivity","udyam.NationalIndustryClassificationCode",
    "udyam.timestamp","udyam.verificationproof",
    "Unique Provider ID",
    "distance.precisionlevel","distance.resulttype","distance.kms",
    "distance.verificationproof",
    "FSSAI vs GST: Jaro-Winkler","FSSAI vs GST: Bigram",
    "FSSAI vs GST: Dice (tokens)",
    "GST vs Udyam: Jaro-Winkler","GST vs Udyam: Bigram",
    "GST vs Udyam: Dice (tokens)",
]

def empty_row():
    return {c: "" for c in MASTER_COLS}

def load_master_db():
    if MASTER_DB_PATH.exists():
        try:
            df = pd.read_excel(MASTER_DB_PATH, dtype=str).fillna("")
            # Add any missing columns
            for c in MASTER_COLS:
                if c not in df.columns:
                    df[c] = ""
            return df
        except Exception as e:
            print(f"  Warning: Could not read master DB: {e}")
    return pd.DataFrame(columns=MASTER_COLS)

def save_master_db(df):
    df = df[MASTER_COLS]  # ensure column order
    df.to_excel(MASTER_DB_PATH, index=False)

# ═══════════════════════════════════════════════════════════════
# INPUT → MASTER DB MAPPING
# ═══════════════════════════════════════════════════════════════
def map_input_to_master(row):
    """Map Input File Format columns → Master DB columns."""
    r = empty_row()
    r["Provider ID"]       = str(row.get("providerID", "")).strip()
    r["Bpp ID"]            = str(row.get("subscriber id", "")).strip()
    r["Sellerapp"]         = str(row.get("Seller App Name", "")).strip()
    r["Domain"]            = str(row.get("ondc_domain", "")).strip()
    r["Provider Name"]     = str(row.get("Seller Name", "")).strip()
    r["TEAM ID"]           = str(row.get("teamID", "")).strip()
    r["Seller Pan No"]     = str(row.get("pan_no", "")).strip().upper()
    r["Server Date"]       = datetime.now().strftime("%Y-%m-%d")
    r["Provider Contact No"] = str(row.get("mobile", "")).strip()
    r["FSSAI.id"]          = str(row.get("fssai", "")).strip()
    r["PAN.id"]            = str(row.get("pan_no", "")).strip().upper()
    r["GST"]               = str(row.get("gstin_no", "")).strip().upper()
    r["udyam.id"]          = str(row.get("udyam_no", "")).strip().upper()
    r["Unique Provider ID"] = f"{sfn(r['Bpp ID'])}_{r['Provider ID']}"
    return r

# ═══════════════════════════════════════════════════════════════
# VALIDATION
# ═══════════════════════════════════════════════════════════════
def validate(row):
    errs = []
    for k, l in [("pan_no","PAN"),("udyam_no","Udyam"),("providerID","Provider ID"),
        ("ondc_domain","ONDC Domain"),("teamID","TEAM ID"),("Seller Name","Seller Name"),
        ("subscriber id","Subscriber ID"),("Seller App Name","Seller App Name")]:
        if not str(row.get(k,"")).strip(): errs.append(f"{l} is mandatory")
    pan = str(row.get("pan_no","")).strip()
    if pan:
        t = pan.upper()
        if len(t) != 10: errs.append(f"PAN must be 10 chars (got {len(t)})")
        elif not re.match(r'^[A-Z]{5}[0-9]{4}[A-Z]$', t): errs.append("PAN format: ABCDE1234F")
    udyam = str(row.get("udyam_no","")).strip()
    if udyam:
        t = udyam.upper()
        if not re.match(r'^UDYAM-[A-Z]{2}-\d{2}-\d{7}$', t):
            c = re.sub(r'[\s-]','',t)
            if not re.match(r'^UDYAM[A-Z]{2}\d{2}\d{7}$', c):
                errs.append("Udyam format: UDYAM-XX-00-0000000")
    domain = str(row.get("ondc_domain","")).strip().upper()
    is_food = "RET10" in domain or "RET11" in domain
    fssai = str(row.get("fssai","")).strip()
    if is_food:
        if not fssai: errs.append("FSSAI mandatory for F&B/Grocery")
        elif not re.match(r'^\d{14}$', fssai.replace(" ","")): errs.append("FSSAI must be 14 digits")
    elif fssai and not re.match(r'^\d{14}$', fssai.replace(" ","")): errs.append("FSSAI must be 14 digits")
    gstin = str(row.get("gstin_no","")).strip()
    if gstin:
        t = gstin.upper()
        if len(t) != 15: errs.append(f"GSTIN must be 15 chars (got {len(t)})")
        elif pan and pan.strip() and t[2:12] != pan.strip().upper(): errs.append("PAN mismatch in GSTIN")
    return len(errs)==0, errs

# ═══════════════════════════════════════════════════════════════
# DIGIO API
# ═══════════════════════════════════════════════════════════════
def _hdr():
    if DIGIO_AUTH:
        a = DIGIO_AUTH if DIGIO_AUTH.startswith("Basic ") else f"Basic {DIGIO_AUTH}"
        return {"Authorization": a, "Content-Type": "application/json"}
    elif DIGIO_ID and DIGIO_SECRET:
        b = base64.b64encode(f"{DIGIO_ID}:{DIGIO_SECRET}".encode()).decode()
        return {"Authorization": f"Basic {b}", "Content-Type": "application/json"}
    return None

def _is_filled(v):
    if v is None: return False
    s = str(v).strip()
    return bool(s) and s.lower() not in ("","none","null","nan","unknown")

def _call_digio(endpoint, body, expected_fields, label):
    """Call Digio API with retry logic. Retries up to MAX_RETRIES if expected fields are empty."""
    h = _hdr()
    if not h:
        print(f"      ⚠ Digio not configured — skipping {label}")
        return {}
    url = f"{DIGIO_URL}{endpoint}"
    best_response = {}
    for attempt in range(1, MAX_RETRIES + 1):
        try:
            print(f"      Attempt {attempt}/{MAX_RETRIES}: POST {url}")
            print(f"        Body: {json.dumps(body)}")
            r = requests.post(url, headers=h, json=body, timeout=30)
            d = r.json()
            print(f"        Response ({r.status_code}): {json.dumps(d, indent=8, default=str)[:500]}")

            # Merge: keep best values across retries
            for k, v in d.items():
                if _is_filled(v) and not _is_filled(best_response.get(k)):
                    best_response[k] = v
                elif isinstance(v, dict):
                    if k not in best_response or not isinstance(best_response[k], dict):
                        best_response[k] = {}
                    for kk, vv in v.items():
                        if _is_filled(vv) and not _is_filled(best_response[k].get(kk)):
                            best_response[k][kk] = vv

            # Check if all expected fields are filled
            all_filled = all(_is_filled(best_response.get(f)) or
                (isinstance(best_response.get(f), dict) and any(_is_filled(v) for v in best_response[f].values()))
                for f in expected_fields)
            if all_filled:
                print(f"      ✓ Complete response on attempt {attempt}")
                return best_response
            missing = [f for f in expected_fields if not _is_filled(best_response.get(f))]
            print(f"      ⚠ Missing fields after attempt {attempt}: {missing}")
            if attempt < MAX_RETRIES:
                time.sleep(1)
        except Exception as e:
            print(f"      ERROR attempt {attempt}: {e}")
            if attempt < MAX_RETRIES:
                time.sleep(2)
    print(f"      ⚠ Returning best response after {MAX_RETRIES} attempts")
    return best_response

def verify_pan(pan_no, name):
    d = _call_digio(
        "/v3/client/kyc/fetch_id_data/PAN",
        {"id_no": pan_no.strip().upper(), "name": name.strip(), "dob": DEFAULT_DOB},
        ["status"],
        "PAN"
    )
    ts = datetime.now().isoformat()
    return {
        "PAN.id": d.get("pan", pan_no.upper()),
        "PAN.status": d.get("status", ""),
        "PAN.timestamp": ts,
    }

def verify_fssai(fssai_no):
    """
    POST /client/v4/apis/kyc/fetch_id_data/FSSAI     (updated endpoint — v4)

    Digio response (Format B — the actual/common shape):
    {
      "fssai_details": [
        {
          "premiseaddress": "...",
          "licenseno": "21523064000396",
          "licensecategoryname": "Registration",
          "statename": "Maharashtra",
          "licenseactiveflag": "Y",           <-- credential report "Status" source
          "statusdesc": "License Issued",
          "companyname": "ACME FOODS PVT LTD",
          "licensecategoryid": 3,
          "fboid": 22955209895357833,
          "displayrefid": "30230322113072882"
        }
      ]
    }

    Legacy Format A (top-level Status / Company Name / Premises Address) is
    still handled below for backward compatibility.
    """
    h = _hdr()
    if not h:
        return {
            "FSSAI.id": fssai_no, "FSSAI.validationstatus": "",
            "FSSAI.premiseaddress": "", "FSSAI.fullpremiseaddress": "",
            "FSSAI.companyname": "", "FSSAI.expiry": "", "FSSAI.timestamp": "",
            "FSSAI.requeststatus": "", "FSSAI.state": "", "fssai.pincode": "",
            "FSSAI.licensestatus": "", "Fssai.applicationtype": "",
            "fssai.licensecategory": "", "fssai.error": "Digio not configured",
        }

    body = {"id_no": fssai_no.strip()}
    # Endpoint updated to the v4 path per the current Digio curl.
    url = f"{DIGIO_URL}/client/v4/apis/kyc/fetch_id_data/FSSAI"
    ts = datetime.now().isoformat()

    # Only 1 attempt — Digio consistently returns the same structure
    # Retrying won't produce different fields
    print(f"      Request: POST {url}")
    print(f"        Body: {json.dumps(body)}")
    try:
        r = requests.post(url, headers=h, json=body, timeout=30)
        d = r.json()
        print(f"        Response ({r.status_code}): {json.dumps(d, indent=8, default=str)[:600]}")

        # Handle BOTH response formats:
        # Format A (legacy top-level): "Status", "Company Name", "Premises Address"
        # Format B (current, nested):  fssai_details[0].licenseactiveflag, etc.

        # Try Format A first (legacy, per old Digio docs)
        if "Status" in d:
            return {
                "FSSAI.id": d.get("License / Registration No.", fssai_no),
                "FSSAI.validationstatus": d.get("Status", ""),
                "FSSAI.premiseaddress": d.get("Premises Address", ""),
                "FSSAI.fullpremiseaddress": d.get("Premises Address", ""),
                "FSSAI.companyname": d.get("Company Name", ""),
                "FSSAI.expiry": d.get("Expiry Date", ""),
                "FSSAI.timestamp": ts,
                "FSSAI.requeststatus": "completed",
                "FSSAI.state": "",
                "fssai.pincode": "",
                "FSSAI.licensestatus": "",
                "Fssai.applicationtype": "",
                "fssai.licensecategory": d.get("Kind of Business", ""),
                "fssai.error": "",
            }

        # Format B: data inside fssai_details array
        details_list = d.get("fssai_details", [])
        if details_list and isinstance(details_list, list) and len(details_list) > 0:
            det = details_list[0]  # take first entry

            # Coerce licenseactiveflag → always a plain string.
            # Digio returns it inconsistently:
            #   - JSON boolean  (True / False)         ← current live behaviour
            #   - JSON string   ("Y" / "N" / "Active") ← older shape
            #   - null / missing                       ← rare
            # pandas' StringDtype on master_seller_database.xlsx rejects
            # non-string values (TypeError: Invalid value 'True' for dtype
            # 'str'), so normalise to a human-readable string right at the
            # source, before it ever reaches the DataFrame.
            _laf = det.get("licenseactiveflag", "")
            if isinstance(_laf, bool):
                _laf_str = "Active" if _laf else "Inactive"
            elif _laf is None:
                _laf_str = ""
            else:
                _laf_str = str(_laf).strip()

            return {
                "FSSAI.id": det.get("licenseno", fssai_no),
                # Credential report "FSSAI Status" is sourced from licenseactiveflag
                "FSSAI.validationstatus": _laf_str,
                "FSSAI.premiseaddress": det.get("premiseaddress", ""),
                "FSSAI.fullpremiseaddress": det.get("premiseaddress", ""),
                # Company name now sourced from Format B (was blank previously)
                "FSSAI.companyname": det.get("companyname", ""),
                "FSSAI.expiry": "",       # not returned in this format
                "FSSAI.timestamp": ts,
                "FSSAI.requeststatus": "completed",
                "FSSAI.state": det.get("statename", ""),
                "fssai.pincode": "",
                "FSSAI.licensestatus": det.get("statusdesc", ""),
                "Fssai.applicationtype": "",
                "fssai.licensecategory": det.get("licensecategoryname", ""),
                "fssai.error": "",
            }

        # Neither format matched — store error
        return {
            "FSSAI.id": fssai_no,
            "FSSAI.validationstatus": "",
            "FSSAI.premiseaddress": "", "FSSAI.fullpremiseaddress": "",
            "FSSAI.companyname": "", "FSSAI.expiry": "", "FSSAI.timestamp": ts,
            "FSSAI.requeststatus": "error", "FSSAI.state": "", "fssai.pincode": "",
            "FSSAI.licensestatus": "", "Fssai.applicationtype": "",
            "fssai.licensecategory": "",
            "fssai.error": f"Unexpected response format: {json.dumps(d)[:200]}",
        }

    except Exception as e:
        print(f"      ERROR: {e}")
        return {
            "FSSAI.id": fssai_no,
            "FSSAI.validationstatus": "", "FSSAI.premiseaddress": "",
            "FSSAI.fullpremiseaddress": "", "FSSAI.companyname": "",
            "FSSAI.expiry": "", "FSSAI.timestamp": ts,
            "FSSAI.requeststatus": "error", "FSSAI.state": "", "fssai.pincode": "",
            "FSSAI.licensestatus": "", "Fssai.applicationtype": "",
            "fssai.licensecategory": "", "fssai.error": str(e),
        }

def verify_udyam(udyam_no):
    """
    POST /client/v4/apis/kyc/fetch_id_data/UDYAMAADHAAR
    Body: {"id_no": "UDYAM-XX-00-0000000"}

    Retry behaviour
    ---------------
    Digio's Udyam endpoint has been observed to flap: the SAME valid Udyam
    number sometimes returns HTTP 200 with data and sometimes HTTP 404
    (or 200 with an empty body). To smooth this out we retry each id-format
    up to MAX_RETRIES times, treating any of the following as a retryable
    failure:
        • HTTP status ≠ 200
        • response body isn't JSON
        • JSON is missing "Udyam Registration Number" / "UAN"
    Small backoff between attempts (1s, 2s, 3s, …) so we don't hammer Digio.
    Returns on the first attempt that yields a usable UAN.
    """
    h = _hdr()
    if not h:
        return {
            "udyam.id": udyam_no.upper(), "udyam.status": "no_credentials",
            "udyam.enterprisetype": "", "udyam.enterprisename": "",
            "udyam.majoractivity": "", "udyam.NationalIndustryClassificationCode": "",
            "udyam.timestamp": "", "udyam.classificationdate": "",
        }

    # Try with hyphens first, then without
    formats = [udyam_no.strip().upper()]
    nh = re.sub(r'[\s-]', '', udyam_no.strip().upper())
    if nh != formats[0]:
        formats.append(nh)

    ts = datetime.now().isoformat()
    url = f"{DIGIO_URL}/client/v4/apis/kyc/fetch_id_data/UDYAMAADHAAR"
    last_status = None
    last_body_snippet = ""

    for fmt in formats:
        body = {"id_no": fmt}
        print(f"      Request: POST {url}")
        print(f"        Body: {json.dumps(body)}")

        for attempt in range(1, MAX_RETRIES + 1):
            try:
                if attempt > 1:
                    print(f"      Retry {attempt}/{MAX_RETRIES} for format {fmt} "
                          f"(previous status={last_status})")
                r = requests.post(url, headers=h, json=body, timeout=30)
                last_status = r.status_code

                # Try to parse JSON; if that fails, still count as retryable.
                try:
                    d = r.json()
                except Exception:
                    d = {}
                    last_body_snippet = (r.text or "")[:300]

                print(f"        Response ({r.status_code}) "
                      f"[attempt {attempt}/{MAX_RETRIES}]: "
                      f"{json.dumps(d, indent=8, default=str)[:600]}")

                # Digio flaps between 200 and 404 for the SAME valid Udyam.
                # Only accept a response that is both 200 AND contains a UAN.
                uan = ""
                if r.status_code == 200 and isinstance(d, dict):
                    uan = d.get("Udyam Registration Number", d.get("UAN", ""))

                if uan:
                    # Enterprise Type is now an array of classifications
                    # Take the first (most recent) entry
                    etype = ""
                    cdate = ""
                    et_list = d.get("Enterprise Type", [])
                    if isinstance(et_list, list) and et_list:
                        etype = et_list[0].get("Enterprise Type", "")
                        cdate = et_list[0].get("Classification Date", "")
                    elif isinstance(et_list, str):
                        # Fallback if it's a plain string (old format)
                        etype = et_list

                    # NIC codes — key name has (S) at the end
                    nic_2 = ""
                    nic_codes = d.get("National Industry Classification Code(S)",
                                d.get("National Industry Classification Code", []))
                    if isinstance(nic_codes, list) and nic_codes:
                        nic_2 = nic_codes[0].get("Nic 2 Digit",
                                nic_codes[0].get("NIC 2 Digit", ""))

                    print(f"      ✓ Udyam verified on attempt {attempt}: {uan}")
                    print(f"        Enterprise: {d.get('Name of Enterprise', '—')}")
                    print(f"        Type: {etype}, Activity: {d.get('Major Activity', '—')}")

                    return {
                        "udyam.id": uan,
                        "udyam.status": "Verified" if etype else "Not Verified",
                        "udyam.enterprisetype": etype,
                        "udyam.enterprisename": d.get("Name of Enterprise", ""),
                        "udyam.majoractivity": d.get("Major Activity", ""),
                        "udyam.NationalIndustryClassificationCode": nic_2,
                        "udyam.classificationdate": cdate,
                        "udyam.timestamp": ts,
                    }

                # Not a usable response — retry (unless we've exhausted attempts)
                if attempt < MAX_RETRIES:
                    reason = ("non-200" if r.status_code != 200
                              else "empty/missing UAN in body")
                    print(f"      ⚠ {reason} — backing off before retry")
                    time.sleep(attempt)   # 1s, 2s, 3s, …

            except Exception as e:
                print(f"      ERROR (format {fmt}, attempt {attempt}): {e}")
                if attempt < MAX_RETRIES:
                    time.sleep(attempt)

        print(f"      ⚠ Exhausted {MAX_RETRIES} attempts for format {fmt} "
              f"(last status={last_status})")

    # No format × no attempt returned a valid registration
    print(f"      ✗ Udyam not resolvable after retries — last status={last_status}")
    return {
        "udyam.id": udyam_no.upper(),
        "udyam.status": "Not Found",
        "udyam.enterprisetype": "", "udyam.enterprisename": "",
        "udyam.majoractivity": "", "udyam.NationalIndustryClassificationCode": "",
        "udyam.timestamp": ts, "udyam.classificationdate": "",
    }

def verify_gst(gstin_no):
    d = _call_digio(
        "/v3/client/kyc/fetch_id_data/GST",
        {"id_no": gstin_no.strip().upper()},
        ["gstin", "corporate_name", "details"],
        "GST"
    )
    ts = datetime.now().isoformat()
    if "error_message" in d:
        return {
            "GST": gstin_no.upper(),
            "gst.status": "",
            "gst.checkstatus": "",
            "gst.legalname": "",
            "gst.duty": "",
            "gst.enterprisetype": "",
            "gst.error": d.get("error_message", ""),
            "gst.registrationdate": "",
            "gst.centraltaxjurisdiction": "",
            "gst.address.flatnumber": "",
            "gst.address.buildingnumber": "",
            "gst.address.buildingname": "",
            "gst.address.buildingname.1": "",
            "gst.address.area": "",
            "gst.address.city": "",
            "gst.address.state": "",
            "gst.address.pincode": "",
            "gst.timestamp": ts,
        }
    det = d.get("details", {})
    if not isinstance(det, dict):
        det = {}
    addr = det.get("pradr", {}).get("addr", {})
    if not isinstance(addr, dict):
        addr = {}
    return {
        "GST": d.get("gstin", gstin_no.upper()),
        "gst.status": det.get("sts", ""),
        "gst.checkstatus": "",  # left empty per your mapping
        "gst.legalname": d.get("corporate_name", ""),
        "gst.duty": det.get("dty", ""),
        "gst.enterprisetype": det.get("ctb", ""),
        "gst.error": "",
        "gst.registrationdate": det.get("rgdt", ""),
        "gst.centraltaxjurisdiction": det.get("ctj", ""),
        "gst.address.flatnumber": addr.get("flno", ""),
        "gst.address.buildingnumber": addr.get("bno", ""),
        "gst.address.buildingname": addr.get("bnm", ""),
        "gst.address.buildingname.1": "",  # left empty per your mapping
        "gst.address.area": addr.get("loc", ""),
        "gst.address.city": addr.get("dst", ""),
        "gst.address.state": addr.get("stcd", ""),
        "gst.address.pincode": addr.get("pncd", ""),
        "gst.timestamp": ts,
    }

def verify_all(row):
    """Run all Digio verifications and return merged dict of master DB fields."""
    merged = {}
    pan = str(row.get("pan_no","")).strip()
    name = str(row.get("Seller Name","")).strip()
    if pan:
        print(f"    [PAN] {pan}")
        merged.update(verify_pan(pan, name))
        print(f"    → {merged.get('PAN.status','?')}\n")
    fssai = str(row.get("fssai","")).strip()
    if fssai:
        print(f"    [FSSAI] {fssai}")
        merged.update(verify_fssai(fssai))
        print(f"    → {merged.get('FSSAI.validationstatus','?')}\n")
    udyam = str(row.get("udyam_no","")).strip()
    if udyam:
        print(f"    [Udyam] {udyam}")
        merged.update(verify_udyam(udyam))
        print(f"    → {merged.get('udyam.status','?')}\n")
    gstin = str(row.get("gstin_no","")).strip()
    if gstin:
        print(f"    [GST] {gstin}")
        merged.update(verify_gst(gstin))
        print(f"    → {merged.get('gst.status','?')}\n")
    return merged

# ═══════════════════════════════════════════════════════════════
# CATALOG SCORING (from on_search JSON)
# ═══════════════════════════════════════════════════════════════
def _norm_name(s):
    """Normalise a filename/stem for tolerant matching (drop spaces/underscores/case)."""
    return re.sub(r'[^a-z0-9]', '', str(s).lower())

def resolve_mapping_file(fname):
    """
    Locate a mapping workbook inside MAPPINGS, tolerant of differences in
    spacing / underscores / capitalisation between the configured name and
    the file actually saved on disk (e.g. 'FB_Category_Mapping.xlsx' vs
    'FB Category Mapping.xlsx' vs 'F_B_Category_Mapping.xlsx').
    Returns a Path or None.
    """
    exact = MAPPINGS / fname
    if exact.exists():
        return exact
    target = _norm_name(Path(fname).stem)
    if MAPPINGS.exists():
        for p in sorted(MAPPINGS.glob("*.xlsx")):
            if _norm_name(p.stem) == target:
                return p
    return None

def load_mapping(path):
    """
    Parse a category mapping workbook into (field_names, json_paths, rules).

    Layout (robust to a leading column offset, e.g. Electronics where the
    grid starts at column F instead of column A):
      • Row 0     : attribute / field display names
      • Row 1     : API-contract JSON paths (each begins with 'message.')
      • Anchor col: category name, one row per category (rows 2+)
      • Grid cells: 'M' = Mandatory, 'O' = Optional, blank/'NR' = Not Required

    The anchor (category) column is detected as the column immediately before
    the first JSON-path column, so files with leading blank columns parse
    correctly. Attribute columns are exactly those carrying a JSON path, which
    also excludes any stray trailing columns.

    Scoring is Mandatory-only: 'rules[category]' is a list of booleans, True
    where the attribute is Mandatory ('M') for that category. Optional ('O')
    and Not-Required (blank) attributes are recorded as False and therefore do
    not affect the completeness score.
    """
    df = pd.read_excel(path, header=None)
    if df.shape[0] < 3:
        return [], [], {}

    # Attribute columns = every column whose row-1 cell is a JSON path.
    row1 = df.iloc[1]
    attr_cols = [ci for ci in range(df.shape[1])
                 if str(row1.iloc[ci]).strip().lower().startswith("message.")]

    if attr_cols:
        cat_col = attr_cols[0] - 1
        if cat_col < 0:
            cat_col = 0
    else:
        # Legacy fallback: category in col A, attributes from col B onward.
        cat_col = 0
        attr_cols = list(range(1, df.shape[1]))

    fn = [str(df.iloc[0, ci]).strip() for ci in attr_cols]
    jp = [str(df.iloc[1, ci]).strip() for ci in attr_cols]

    rules = {}
    for ri in range(2, df.shape[0]):
        cat = str(df.iloc[ri, cat_col]).strip()
        if not cat or cat.lower() == "nan":
            continue
        rules[cat] = [str(df.iloc[ri, ci]).strip().upper() == "M" for ci in attr_cols]
    return fn, jp, rules

def _rp(obj, path):
    parts = re.split(r'\.(?![^[]*\])', path)
    c = obj
    for p in parts:
        if c is None: return None
        m = re.match(r'^(.+?)\[(\d+)\]$', p)
        if m:
            k, i = m.group(1), int(m.group(2))
            if isinstance(c,dict) and k in c:
                a = c[k]; c = a[i] if isinstance(a,list) and i<len(a) else None
            else: return None
        elif isinstance(c,dict): c = c.get(p)
        elif isinstance(c,list) and c and isinstance(c[0],dict): c = c[0].get(p)
        else: return None
    return c

def resolve_path(item, path, prov=None):
    pfx = ["message.catalog.bpp/providers.items.","message.catalog.bpp/providers[0].items[0].","message.catalog.bpp/providers[0].items.","message.catalog.bpp/providers.items[0]."]
    target, stripped = item, path
    for p in pfx:
        if path.startswith(p): stripped = path[len(p):]; target = item; break
    else:
        pp = "message.catalog.bpp/providers."
        if path.startswith(pp) and "items." not in path.split(pp)[1].split('.')[0]:
            stripped = path[len(pp):]; target = prov if prov else item
    tm = re.match(r'^tags\[(\d+)\]\.(.+)$', stripped)
    if tm:
        for tb in target.get("tags",[]):
            r = _rp(tb, tm.group(2))
            if r is not None: return r
        return None
    return _rp(target, stripped)

def is_filled(v):
    if v is None: return False
    if isinstance(v,bool): return True
    if isinstance(v,(int,float)): return not(isinstance(v,float) and math.isnan(v))
    if isinstance(v,(list,dict)): return len(v)>0
    s = str(v).strip()
    return bool(s) and s.lower() not in ("na","nan","none","null","")

def score_item(item, fn, jp, mand, prov=None):
    mc = sum(1 for m in mand if m)
    if mc == 0: return 100.0, 0, 0
    fc = sum(1 for f, j, m in zip(fn, jp, mand) if m and is_filled(resolve_path(item, j, prov)))
    return round(fc/mc*100, 2), fc, mc

def _match_rules(cid, rules, fn):
    """
    Pick the rule row for an item's category_id.
    Two-pass: exact (case-insensitive) match first, then substring match,
    then fall back to the first category's rules.
    """
    cl = str(cid).lower().strip()
    if cl:
        for cn, rv in rules.items():
            if cn.lower().strip() == cl:
                return rv
        for cn, rv in rules.items():
            cnl = cn.lower().strip()
            if cnl and (cl in cnl or cnl in cl):
                return rv
    return next(iter(rules.values()), [False] * len(fn))

def catalog_scores(on_search, domain_raw, pid):
    d = dc(domain_raw)
    if not d: return {"aggregate_score":0,"num_skus":0,"skus":[],"error":f"Unknown domain: {domain_raw}"}
    mp = resolve_mapping_file(d["m"])
    if not mp: return {"aggregate_score":0,"num_skus":0,"skus":[],"error":f"Mapping not found: {d['m']}"}
    fn, jp, rules = load_mapping(str(mp))
    if not rules: return {"aggregate_score":0,"num_skus":0,"skus":[],"error":f"No category rules parsed from {mp.name}"}
    provs = []
    if isinstance(on_search, dict):
        msg = on_search.get("message", on_search)
        cat = msg.get("catalog", msg)
        provs = cat.get("bpp/providers", cat.get("providers", []))
    elif isinstance(on_search, list): provs = on_search
    tp = None
    for p in provs:
        pi = p.get("id","")
        if pi == pid or pid.lower() in pi.lower() or pi.lower() in pid.lower():
            tp = p; break
    if not tp: return {"aggregate_score":0,"num_skus":0,"skus":[],"error":f"Provider {pid} not found in on_search"}
    items = tp.get("items",[])
    if not items: return {"aggregate_score":0,"num_skus":0,"skus":[],"error":"No items for provider"}
    skus = []
    for item in items:
        iid = item.get("id","?")
        cid = item.get("category_id","")
        r = _match_rules(cid, rules, fn)
        sc, fc, mc = score_item(item, fn, jp, r, tp)
        skus.append({"sku_id":iid,"category_id":cid,"completeness_score":sc,"filled":fc,"total_mandatory":mc})
    avg = round(sum(s["completeness_score"] for s in skus)/len(skus),2) if skus else 0
    return {"aggregate_score":avg,"num_skus":len(skus),"skus":skus,
        "above_80":sum(1 for s in skus if s["completeness_score"]>=80),
        "below_50":sum(1 for s in skus if s["completeness_score"]<50)}

# ═══════════════════════════════════════════════════════════════
# PDF GENERATION
# ═══════════════════════════════════════════════════════════════
def sf(v):
    if v is None: return ""
    if isinstance(v,float) and math.isnan(v): return ""
    s = str(v).strip()
    return "" if s.lower() in ("nan","none") else s

def sfn(v): return re.sub(r"[^A-Za-z0-9._-]+","_", sf(v) or "X").strip("_") or "X"

def _sty():
    s = getSampleStyleSheet()
    s.add(ParagraphStyle("m2",parent=s["Normal"],fontName="Helvetica-Bold",fontSize=10,leading=14,textColor=colors.HexColor("#333")))
    s.add(ParagraphStyle("t3",parent=s["Title"],fontName="Helvetica-Bold",fontSize=15,leading=20,textColor=colors.HexColor("#0B3D91"),alignment=TA_LEFT,spaceBefore=10,spaceAfter=10))
    s.add(ParagraphStyle("s3",parent=s["Heading2"],fontName="Helvetica-Bold",fontSize=12,leading=16,textColor=colors.HexColor("#0B3D91"),spaceBefore=12,spaceAfter=6))
    s.add(ParagraphStyle("sb3",parent=s["Heading3"],fontName="Helvetica-Bold",fontSize=11,leading=14,textColor=colors.HexColor("#222"),spaceBefore=8,spaceAfter=4))
    s.add(ParagraphStyle("ag2",parent=s["Normal"],fontName="Helvetica",fontSize=12,leading=16,spaceBefore=4,spaceAfter=4))
    return s

def _lvt(rows, sty):
    data = [[Paragraph(f"<b>{l}</b>",sty["Normal"]),Paragraph(v if v else "—",sty["Normal"])] for l,v in rows]
    t = Table(data, colWidths=[7*cm,9*cm])
    t.setStyle(TableStyle([("VALIGN",(0,0),(-1,-1),"TOP"),("LEFTPADDING",(0,0),(-1,-1),6),("RIGHTPADDING",(0,0),(-1,-1),6),("TOPPADDING",(0,0),(-1,-1),4),("BOTTOMPADDING",(0,0),(-1,-1),4),("BACKGROUND",(0,0),(0,-1),colors.HexColor("#F2F4F8")),("LINEBELOW",(0,0),(-1,-1),0.25,colors.HexColor("#D0D5DD")),("BOX",(0,0),(-1,-1),0.5,colors.HexColor("#D0D5DD"))]))
    return t

def _hdr_pdf(story, sty, rtype, app, pid, date):
    vid = f"ONDC/{rtype}/{app}/{pid}"
    left = [Paragraph(f"<b>Date of report generated:</b> {date}",sty["m2"]),Spacer(1,0.15*cm),Paragraph(f"<b>Validation ID:</b><br/>{vid}",sty["m2"])]
    lp = SD / LOGO
    right = Image(str(lp),width=3.8*cm,height=1.6*cm,kind="proportional") if lp.exists() else Paragraph("",sty["m2"])
    ht = Table([[left,right]],colWidths=[11*cm,5*cm])
    ht.setStyle(TableStyle([("VALIGN",(0,0),(-1,-1),"TOP"),("ALIGN",(1,0),(1,0),"RIGHT"),("LEFTPADDING",(0,0),(-1,-1),0),("RIGHTPADDING",(0,0),(-1,-1),0),("TOPPADDING",(0,0),(-1,-1),0),("BOTTOMPADDING",(0,0),(-1,-1),0)]))
    story.append(ht); story.append(Spacer(1,0.4*cm))

def build_cred_pdf(mr, path, date):
    """Build credential PDF from master DB row."""
    doc = SimpleDocTemplate(str(path),pagesize=A4,leftMargin=2*cm,rightMargin=2*cm,topMargin=2*cm,bottomMargin=2*cm)
    s = _sty(); st = []
    _hdr_pdf(st, s, "Credential Report", sf(mr.get("Sellerapp")), sf(mr.get("Provider ID")), date)
    st.append(Paragraph("ONDC Credential Validation Report", s["t3"]))
    st.append(Paragraph("Provider Summary", s["s3"]))
    st.append(_lvt([("Provider Name",sf(mr.get("Provider Name"))),("Seller App(s)",sf(mr.get("Sellerapp"))),("Provider ID (As per Seller App)",sf(mr.get("Provider ID"))),("Categories subscribed to on ONDC Network",dl(mr.get("Domain","")))], s))
    st.append(Paragraph("Seller credentials and verification status", s["s3"]))
    st.append(Paragraph("PAN", s["sb3"]))
    st.append(_lvt([("PAN Number",sf(mr.get("PAN.id"))),("PAN Validity Check",sf(mr.get("PAN.status")))], s))
    st.append(Paragraph("GSTIN", s["sb3"]))
    st.append(_lvt([("GSTIN Number",sf(mr.get("GST"))),("GSTIN Validity Check",sf(mr.get("gst.status"))),("Legal Name",sf(mr.get("gst.legalname"))),("Company Type",sf(mr.get("gst.enterprisetype"))),("Registration Date",sf(mr.get("gst.registrationdate")))], s))
    st.append(Paragraph("Udyam", s["sb3"]))
    st.append(_lvt([("Udyam Number",sf(mr.get("udyam.id"))),("Udyam Validity Check",sf(mr.get("udyam.status"))),("Enterprise Name",sf(mr.get("udyam.enterprisename"))),("Enterprise Type",sf(mr.get("udyam.enterprisetype"))),("Major Activity",sf(mr.get("udyam.majoractivity")))], s))
    st.append(Paragraph("FSSAI Registration Verification", s["sb3"]))
    # FSSAI Status row is populated from mr["FSSAI.validationstatus"], which
    # verify_fssai() now sources from fssai_details[0].licenseactiveflag.
    st.append(_lvt([("License Number",sf(mr.get("FSSAI.id"))),("FSSAI Status",sf(mr.get("FSSAI.validationstatus"))),("Company Name",sf(mr.get("FSSAI.companyname"))),("Expiry Date",sf(mr.get("FSSAI.expiry")))], s))
    doc.build(st)

def build_cat_pdf(pd_, path, date):
    doc = SimpleDocTemplate(str(path),pagesize=A4,leftMargin=2*cm,rightMargin=2*cm,topMargin=2*cm,bottomMargin=2*cm)
    s = _sty(); st = []
    _hdr_pdf(st, s, "Catalog Score Report", pd_["seller_app"], pd_["provider_id"], date)
    st.append(Paragraph("ONDC Catalog Score Report", s["t3"]))
    st.append(Paragraph("Provider Summary", s["s3"]))
    st.append(_lvt([("Provider Name",pd_["provider_name"]),("Seller App(s)",pd_["seller_app"]),("Provider ID (as per Seller App)",pd_["provider_id"]),("Categories subscribed to on ONDC Network",pd_["category"]),("Number of SKUs",str(pd_["num_skus"]))], s))
    st.append(Paragraph("Aggregate Catalogue Score", s["s3"]))
    sc = pd_["aggregate_score"]
    st.append(Paragraph(f"Completeness Score out of 100 (Avg. across categories): {int(sc) if sc==int(sc) else f'{sc:.2f}'}", s["ag2"]))
    st.append(Paragraph("SKU-Wise Catalog Score", s["s3"]))
    for i, sku in enumerate(pd_["skus"],1):
        sc2 = sku["completeness_score"]
        st.append(Paragraph(f"SKU - {i} Wise Catalog Score", s["sb3"]))
        st.append(_lvt([("SKU ID",sku["sku_id"]),("Completeness Score out of 100",str(int(sc2) if sc2==int(sc2) else f'{sc2:.2f}'))], s))
    doc.build(st)

# ═══════════════════════════════════════════════════════════════
# GOOGLE DRIVE
# ═══════════════════════════════════════════════════════════════
def get_drive_cred():
    """Get DriveUploader for credential reports folder."""
    if not DRIVE_OK:
        return None
    try:
        return DriveUploader(parent_folder_id=DRIVE_CRED, script_dir=str(SD))
    except Exception as e:
        print(f"  ⚠ Drive auth failed: {e}")
        return None

def get_drive_cat():
    """Get DriveUploader for catalog reports folder."""
    if not DRIVE_OK:
        return None
    try:
        return DriveUploader(parent_folder_id=DRIVE_CAT, script_dir=str(SD))
    except Exception as e:
        print(f"  ⚠ Drive auth failed: {e}")
        return None
# ═══════════════════════════════════════════════════════════════
# MAIN
# ═══════════════════════════════════════════════════════════════
def prompt(txt, must_exist=False):
    while True:
        r = input(txt).strip().strip("'\"")
        if not r: print("  Cannot be empty."); continue
        if must_exist and not os.path.exists(r): print(f"  Not found: {r}"); continue
        return r

def scan_onsearch_folder(folder_path):
    """
    Scan a folder (and all subfolders) for on_search JSON files.
    Pre-index all providers across all JSONs for O(1) lookup.
    Defensive: silently skips files with null/missing providers and reports which.
    """
    index = {}
    folder = Path(folder_path)
    json_files = list(folder.rglob("*.json"))
    print(f"  Found {len(json_files)} JSON file(s) in {folder_path}")

    skipped = []
    for jf in json_files:
        try:
            with open(jf, "r", encoding="utf-8") as f:
                data = json.load(f)
        except Exception as e:
            skipped.append((jf.name, f"parse error: {e}"))
            continue

        # Defensive extraction — every step coerces None to something iterable
        providers = []
        if isinstance(data, dict):
            msg = data.get("message") or {}
            if not isinstance(msg, dict):
                msg = {}
            catalog = msg.get("catalog") or msg
            if not isinstance(catalog, dict):
                catalog = {}
            providers = catalog.get("bpp/providers") or catalog.get("providers") or []
            if not isinstance(providers, list):
                providers = []
        elif isinstance(data, list):
            providers = data

        if not providers:
            skipped.append((jf.name, "no providers (null / empty / NACK)"))
            continue

        for prov in providers:
            if not isinstance(prov, dict):
                continue
            pid = prov.get("id", "")
            if pid and pid not in index:
                index[pid] = {"data": data, "file": str(jf), "provider": prov}

    print(f"  Indexed {len(index)} unique provider(s) across all JSONs")
    if skipped:
        print(f"  ⚠ Skipped {len(skipped)} file(s) with no usable providers:")
        for name, reason in skipped[:10]:
            print(f"    - {name}: {reason}")
        if len(skipped) > 10:
            print(f"    ... and {len(skipped) - 10} more")
    return index

def _read_sellers(path):
    """
    Read the seller list, tolerant of common real-world file quirks.

    Handles:
      • .xlsx / .xls passed instead of .csv (uses read_excel)
      • CSVs whose delimiter is ; or tab or | (not just ,)
      • CSVs with a preamble/title row above the real header
        (auto-detects by finding the first line that yields the max column count)
      • Mixed encodings (utf-8 with/without BOM, latin-1, cp1252)

    Returns list[dict] — one row per seller.
    """
    p = Path(path)
    ext = p.suffix.lower()

    # 1) Excel input — read directly.
    if ext in (".xlsx", ".xls", ".xlsm"):
        try:
            return pd.read_excel(path, dtype=str).fillna("").to_dict("records")
        except Exception as e:
            raise RuntimeError(f"Could not read Excel file {path}: {e}")

    # 2) CSV/TSV/…: try each encoding × each delimiter combo, plus a
    #    skiprows sweep to skip any preamble/title lines.
    encodings = ("utf-8-sig", "utf-8", "latin-1", "cp1252")
    delimiters = (",", ";", "\t", "|")

    last_err = None
    for enc in encodings:
        for delim in delimiters:
            for skip in (0, 1, 2, 3):     # tolerate up to 3 preamble lines
                try:
                    df = pd.read_csv(
                        path, dtype=str, encoding=enc, sep=delim,
                        skiprows=skip, engine="python",
                        on_bad_lines="skip",
                    ).fillna("")
                    # Require ≥2 columns AND at least one recognisable seller field
                    # so we don't accept a garbage 1-column parse of a mis-delimited file.
                    if df.shape[1] >= 2 and any(
                        c.strip().lower() in ("providerid", "seller name",
                                              "pan_no", "gstin_no",
                                              "udyam_no", "ondc_domain",
                                              "subscriber id", "seller app name")
                        for c in df.columns
                    ):
                        return df.to_dict("records")
                except Exception as e:
                    last_err = e
                    continue

    # 3) Last resort — maybe it really IS an .xlsx renamed .csv
    try:
        return pd.read_excel(path, dtype=str).fillna("").to_dict("records")
    except Exception:
        pass

    raise RuntimeError(
        f"Could not parse seller file: {path}\n"
        f"  Tried encodings: {encodings}\n"
        f"  Tried delimiters: {', '.join(repr(d) for d in delimiters)}\n"
        f"  Tried skipping 0-3 preamble rows\n"
        f"  Also tried reading as Excel.\n"
        f"  Last error: {last_err}\n"
        f"  Please open the file in a plain text editor and check that:\n"
        f"    (a) it's actually a text file (not xlsx binary),\n"
        f"    (b) column headers include at least one of: providerID, Seller Name,\n"
        f"        pan_no, gstin_no, udyam_no, ondc_domain, subscriber id, Seller App Name."
    )


def main():
    print("=" * 60)
    print("  ONDC TEAM Scheme — Master Pipeline v5")
    print("  Two-pass: Credentials first, then Catalog scoring")
    print("=" * 60)

    # Check Digio
    if DIGIO_AUTH:
        print(f"\n  Digio: Configured via DIGIO_AUTHORIZATION ({DIGIO_URL})")
    elif DIGIO_ID:
        print(f"\n  Digio: Configured via CLIENT_ID ({DIGIO_URL})")
    else:
        print("\n  Digio: NOT configured — credential verification will be skipped")

    # Check Drive
    drive_cred = get_drive_cred()
    drive_cat = get_drive_cat()
    drive_ok = drive_cred is not None
    print(f"  Drive: {'Connected' if drive_ok else 'Not connected'}")

    # Check Supabase
    print(f"  Supabase: {'Available' if SUPABASE_OK else 'Not configured'}")

    # Check mappings
    if MAPPINGS.exists():
        print(f"  Mappings: {len(list(MAPPINGS.glob('*.xlsx')))} found")
    else:
        print(f"  Mappings: Not found at {MAPPINGS}")
    print(f"  Master DB: {MASTER_DB_PATH}")

    # Prompt for inputs
    csv_path = prompt("\n1. Seller CSV path: ", True)
    onsearch_folder = prompt("2. on_search folder path (contains subfolders with JSONs): ", True)
    out_dir = prompt("3. Output folder: ")
    os.makedirs(out_dir, exist_ok=True)

    # Read seller list — tolerant of xlsx passed as csv, alternate delimiters
    # (comma / semicolon / tab / pipe), preamble/title rows above the real
    # header, and mixed encodings (utf-8 with/without BOM, latin-1, cp1252).
    sellers = _read_sellers(csv_path)
    print(f"\n  {len(sellers)} seller(s) loaded")

    today = datetime.now()
    dfn = today.strftime("%d%m%Y")
    ddisp = today.strftime("%d %B %Y")

    # Load master DB
    master_df = load_master_db()
    print(f"  Master DB: {len(master_df)} existing records")

    # ══════════════════════════════════════════════════════════
    # PASS 1: CREDENTIALS (all sellers)
    # ══════════════════════════════════════════════════════════
    print(f"\n{'=' * 60}")
    print(f"  PASS 1: Credential Verification ({len(sellers)} sellers)")
    print(f"{'=' * 60}\n")

    cred_results = []          # successful credential results
    failed_results = []        # validation failures
    cred_manifest_data = {}    # seller_app → {providers: [], pdfs: []}
    supabase_records = []      # for Supabase upload

    for i, row in enumerate(sellers, 1):
        pid = str(row.get("providerID", "")).strip()
        name = str(row.get("Seller Name", "")).strip()
        app = str(row.get("Seller App Name", "")).strip()
        domain = str(row.get("ondc_domain", "")).strip()
        team_id = str(row.get("teamID", "")).strip()
        dcfg = dc(domain) or {"l": domain, "s": "", "m": ""}

        print(f"[{i}/{len(sellers)}] {name} ({pid}) — {app} — {dcfg['l']}")

        # Validate
        ok, errs = validate(row)
        if not ok:
            print(f"  ✗ VALIDATION FAILED:")
            for e in errs:
                print(f"    - {e}")
            failed_results.append({"provider_id": pid, "seller_name": name, "status": "failed", "errors": errs})
            print()
            continue
        print(f"  ✓ Validated")

        # Map input to master DB row
        mr = map_input_to_master(row)

        # Digio verification
        print(f"  Verifying via Digio...")
        cred_fields = verify_all(row)
        mr.update(cred_fields)

        # Update master DB incrementally
        uid = mr["Unique Provider ID"]
        existing_idx = master_df.index[master_df["Unique Provider ID"] == uid].tolist()
        if existing_idx:
            idx = existing_idx[0]
            for col in MASTER_COLS:
                new_val = mr.get(col, "")
                # Skip empty updates so we don't wipe an existing value
                # with a blank returned by a later run.
                if new_val is None or new_val == "":
                    continue
                # Master DB is stored as StringDtype — coerce any stray
                # non-string (bool, int, float, dict) to str so the
                # assignment below can never raise 'Invalid value ... for
                # dtype str'. Belt-and-braces alongside the source-side
                # coercion in verify_fssai().
                if not isinstance(new_val, str):
                    new_val = str(new_val)
                master_df.at[idx, col] = new_val
            print(f"  ✓ Master DB: updated existing record")
        else:
            new_row = pd.DataFrame([mr])
            master_df = pd.concat([master_df, new_row], ignore_index=True)
            print(f"  ✓ Master DB: new record added")

        # Generate credential PDF
        bc = sfn(app)
        pc = sfn(pid)
        cf = Path(out_dir) / f"{bc}_CredentialReport"
        cf.mkdir(parents=True, exist_ok=True)
        cfn = f"ONDC_CredentialReport_{pc}_{dfn}.pdf"
        cp = cf / cfn
        build_cred_pdf(mr, cp, ddisp)
        print(f"  ✓ Credential PDF: {cp}")

        # Upload credential PDF to Drive
        cdu = ""
        if drive_ok:
            try:
                cred_folder_name = f"{bc}_CredentialReport"
                cred_folder_id = drive_cred.get_or_create_folder(cred_folder_name)
                result = drive_cred.upload_file(str(cp), cfn, cred_folder_id)
                cdu = result.get("webViewLink", "")
                print(f"  ✓ Credential PDF uploaded to Drive")
            except Exception as e:
                print(f"  ⚠ Credential Drive upload failed: {e}")

        # Track manifest data
        if app not in cred_manifest_data:
            cred_manifest_data[app] = {"providers": [], "pdfs": []}
        cred_manifest_data[app]["pdfs"].append((str(cp), cfn, pid))
        cred_manifest_data[app]["providers"].append({
            "provider_id": pid,
            "provider_name": name,
            "domain": dl(domain),
            "credential_validation_id": f"ONDC/Credential Report/{app}/{pid}",
            "timestamp": today.isoformat(),
            "pdf_drive_url": cdu,
            "credentials": {
                "pan": {"id": sf(mr.get("PAN.id")), "status": sf(mr.get("PAN.status"))},
                "gst": {"id": sf(mr.get("GST")), "status": sf(mr.get("gst.status"))},
                "udyam": {"id": sf(mr.get("udyam.id")), "status": sf(mr.get("udyam.status")),
                          "major_activity": sf(mr.get("udyam.majoractivity"))},
                "fssai": {"id": sf(mr.get("FSSAI.id")), "status": sf(mr.get("FSSAI.validationstatus"))},
            },
        })

        # Collect Supabase data
        supabase_records.append({
            "Provider ID": pid,
            "TEAM ID": team_id,
            "Udyam Number": sf(mr.get("udyam.id")),
            "Udyam Verification Status": sf(mr.get("udyam.status")),
            "Major Activity": sf(mr.get("udyam.majoractivity")),
            "Enterprise Type": sf(mr.get("udyam.enterprisetype")),
            "Verified by": "Digio",
            "Verification timestamp": sf(mr.get("udyam.timestamp")),
        })

        cred_results.append({
            "provider_id": pid, "seller_name": name, "seller_app": app,
            "domain": domain, "domain_label": dcfg["l"], "team_id": team_id,
            "mr": mr, "bc": bc, "pc": pc, "dcfg": dcfg,
            "credential_report": str(cp), "credential_drive_url": cdu,
        })
        print()

    # ── Upload credential manifests ──
    manifests_dir = Path(out_dir) / "manifests"
    manifests_dir.mkdir(parents=True, exist_ok=True)

    for seller_app, mdata in cred_manifest_data.items():
        bc = sfn(seller_app)
        manifest = {
            "seller_app": seller_app,
            "report_type": "credential",
            "generated_at": today.isoformat(),
            "total_providers": len(mdata["providers"]),
            "providers": mdata["providers"],
        }
        mfn = f"ONDC_CredentialManifest_{bc}_{dfn}.json"
        mpath = manifests_dir / mfn
        with open(mpath, "w", encoding="utf-8") as mf:
            json.dump(manifest, mf, indent=2, default=str)
        if drive_ok:
            try:
                cred_folder_name = f"{bc}_CredentialReport"
                cred_folder_id = drive_cred.get_or_create_folder(cred_folder_name)
                drive_cred.upload_file(str(mpath), mfn, cred_folder_id)
                print(f"  ✓ Credential manifest uploaded: {mfn}")
            except Exception as e:
                print(f"  ⚠ Credential manifest upload failed: {e}")
        else:
            print(f"  ✓ Credential manifest saved locally: {mpath}")

    # ── Save master DB ──
    save_master_db(master_df)
    print(f"  ✓ Master DB saved: {MASTER_DB_PATH} ({len(master_df)} total records)")

    # ── Supabase upload ──
    if supabase_records:
        if SUPABASE_OK:
            print(f"\n  Uploading {len(supabase_records)} record(s) to Supabase...")
            ok_count, fail_count = supabase_upload(supabase_records)
            print(f"  ✓ Supabase: {ok_count} uploaded, {fail_count} failed")
        sb_path = Path(out_dir) / f"supabase_upload_{dfn}.csv"
        pd.DataFrame(supabase_records).to_csv(sb_path, index=False)
        print(f"  ✓ Supabase CSV saved: {sb_path}")

    print(f"\n  Pass 1 complete: {len(cred_results)} credential reports generated")

    # ══════════════════════════════════════════════════════════
    # PASS 2: CATALOG SCORING (from on_search folder)
    # ══════════════════════════════════════════════════════════
    print(f"\n{'=' * 60}")
    print(f"  PASS 2: Catalog Scoring")
    print(f"{'=' * 60}\n")

    # Pre-index all on_search JSONs
    print(f"  Scanning on_search folder: {onsearch_folder}")
    onsearch_index = scan_onsearch_folder(onsearch_folder)

    if not onsearch_index:
        print(f"  ⚠ No providers found in on_search folder — skipping catalog scoring")
    else:
        cat_manifest_data = {}
        cat_count = 0
        skip_count = 0

        for cr in cred_results:
            pid = cr["provider_id"]
            name = cr["seller_name"]
            app = cr["seller_app"]
            domain = cr["domain"]
            bc = cr["bc"]
            pc = cr["pc"]
            dcfg = cr["dcfg"]

            # O(1) lookup in pre-built index
            if pid not in onsearch_index:
                skip_count += 1
                continue

            entry = onsearch_index[pid]
            print(f"  [{cat_count + 1}] {name} ({pid}) — found in {Path(entry['file']).name}")

            # Compute catalog scores using the indexed on_search data
            cat = catalog_scores(entry["data"], domain, pid)
            if cat.get("error"):
                print(f"    ⚠ {cat['error']} — skipping")
                skip_count += 1
                continue
            if cat["num_skus"] == 0:
                print(f"    ⚠ No SKUs — skipping")
                skip_count += 1
                continue

            print(f"    ✓ {cat['num_skus']} SKUs — avg: {cat['aggregate_score']}%")

            # Generate catalog PDF
            catf = Path(out_dir) / f"{bc}_CatalogReport{dcfg.get('s', '')}"
            catf.mkdir(parents=True, exist_ok=True)
            catfn = f"ONDC_CatalogReport_{pc}_{dfn}.pdf"
            catp = catf / catfn
            build_cat_pdf({
                "provider_id": pid, "provider_name": name, "seller_app": app,
                "category": dcfg["l"], "num_skus": cat["num_skus"],
                "aggregate_score": cat["aggregate_score"], "skus": cat.get("skus", [])
            }, catp, ddisp)
            print(f"    ✓ Catalog PDF: {catp}")

            # Upload catalog PDF to Drive
            catu = ""
            if drive_ok:
                try:
                    cat_folder_name = f"{bc}_CatalogReport{dcfg.get('s', '')}"
                    cat_folder_id = drive_cat.get_or_create_folder(cat_folder_name)
                    result = drive_cat.upload_file(str(catp), catfn, cat_folder_id)
                    catu = result.get("webViewLink", "")
                    print(f"    ✓ Catalog PDF uploaded to Drive")
                except Exception as e:
                    print(f"    ⚠ Catalog Drive upload failed: {e}")

            # Track catalog manifest
            if app not in cat_manifest_data:
                cat_manifest_data[app] = {"providers": [], "pdfs": []}
            cat_manifest_data[app]["pdfs"].append((str(catp), catfn, pid))
            cat_manifest_data[app]["providers"].append({
                "provider_id": pid,
                "provider_name": name,
                "domain": dl(domain),
                "catalog_validation_id": f"ONDC/Catalog Score Report/{app}/{pid}",
                "timestamp": today.isoformat(),
                "pdf_drive_url": catu,
                "aggregate_score": cat["aggregate_score"],
                "num_skus": cat["num_skus"],
            })

            # Update the credential result with catalog data
            cr["catalog_report"] = str(catp)
            cr["catalog_drive_url"] = catu
            cr["catalog_score"] = cat["aggregate_score"]
            cr["num_skus"] = cat["num_skus"]
            cat_count += 1
            print()

        # ── Upload catalog manifests ──
        for seller_app, mdata in cat_manifest_data.items():
            bc = sfn(seller_app)
            manifest = {
                "seller_app": seller_app,
                "report_type": "catalog",
                "generated_at": today.isoformat(),
                "total_providers": len(mdata["providers"]),
                "providers": mdata["providers"],
            }
            mfn = f"ONDC_CatalogManifest_{bc}_{dfn}.json"
            mpath = manifests_dir / mfn
            with open(mpath, "w", encoding="utf-8") as mf:
                json.dump(manifest, mf, indent=2, default=str)
            if drive_ok:
                try:
                    cat_folder_name = f"{bc}_CatalogReport{dcfg.get('s', '') if dcfg else ''}"
                    cat_folder_id = drive_cat.get_or_create_folder(cat_folder_name)
                    drive_cat.upload_file(str(mpath), mfn, cat_folder_id)
                    print(f"  ✓ Catalog manifest uploaded: {mfn}")
                except Exception as e:
                    print(f"  ⚠ Catalog manifest upload failed: {e}")
            else:
                print(f"  ✓ Catalog manifest saved locally: {mpath}")

        print(f"\n  Pass 2 complete: {cat_count} catalog reports generated, {skip_count} skipped")

    # ══════════════════════════════════════════════════════════
    # FINAL SUMMARY
    # ══════════════════════════════════════════════════════════

    # Dashboard JSON
    final_results = []
    for cr in cred_results:
        final_results.append({
            "provider_id": cr["provider_id"],
            "seller_name": cr["seller_name"],
            "seller_app": cr["seller_app"],
            "domain": cr["domain"],
            "domain_label": cr["domain_label"],
            "team_id": cr.get("team_id", ""),
            "status": "success",
            "credential_report": cr.get("credential_report", ""),
            "credential_drive_url": cr.get("credential_drive_url", ""),
            "catalog_report": cr.get("catalog_report", ""),
            "catalog_drive_url": cr.get("catalog_drive_url", ""),
            "catalog_score": cr.get("catalog_score", 0),
            "num_skus": cr.get("num_skus", 0),
        })
    for fr in failed_results:
        final_results.append(fr)

    dp = Path(out_dir) / f"pipeline_dashboard_{dfn}.json"
    with open(dp, "w", encoding="utf-8") as f:
        json.dump({
            "generated_at": today.isoformat(),
            "total_sellers": len(sellers),
            "credentials_processed": len(cred_results),
            "credentials_failed": len(failed_results),
            "catalogs_generated": sum(1 for r in final_results if r.get("catalog_report")),
            "results": final_results,
        }, f, indent=2, default=str)

    print(f"\n{'=' * 60}")
    print(f"  PIPELINE COMPLETE")
    print(f"  Credentials: {len(cred_results)} processed, {len(failed_results)} failed")
    cat_gen = sum(1 for r in final_results if r.get("catalog_report"))
    if cat_gen:
        avg = sum(r.get("catalog_score", 0) for r in final_results if r.get("catalog_report")) / cat_gen
        print(f"  Catalogs: {cat_gen} generated, avg score: {avg:.1f}%")
    print(f"  Master DB: {MASTER_DB_PATH}")
    print(f"  Dashboard: {dp}")
    print(f"{'=' * 60}")


if __name__ == "__main__":
    main()