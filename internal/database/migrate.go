package database

import (
	"fmt"
	"log"

	"gorm.io/gorm"
)

func RunMigrations(db *gorm.DB) error {
	log.Println("running database migrations...")

	migrations := []struct {
		name string
		sql  string
	}{
		{"enable uuid extension", `CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`},
		{"create enum_types", `
			CREATE TABLE IF NOT EXISTS enum_types (
				id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
				category    TEXT NOT NULL,
				value       TEXT NOT NULL,
				label       TEXT NOT NULL,
				json_schema JSONB,
				description TEXT NOT NULL DEFAULT '',
				active      BOOLEAN NOT NULL DEFAULT TRUE,
				created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				UNIQUE(category, value)
			)
		`},
		{"add missing enum_types columns", `
			ALTER TABLE enum_types ADD COLUMN IF NOT EXISTS json_schema JSONB;
			ALTER TABLE enum_types ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
			ALTER TABLE enum_types ADD COLUMN IF NOT EXISTS active BOOLEAN NOT NULL DEFAULT TRUE;
		`},
		{"create credential_requests", `
			CREATE TABLE IF NOT EXISTS credential_requests (
				id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
				request_id          UUID NOT NULL,
				cred_type           UUID NOT NULL REFERENCES enum_types(id),
				verification_status UUID NOT NULL REFERENCES enum_types(id),
				retry_count         INTEGER NOT NULL DEFAULT 0,
				max_retries         INTEGER NOT NULL DEFAULT 3,
				cred_data           JSONB NOT NULL,
				evidences           JSONB,
				verification_errors JSONB,
				issuer              UUID REFERENCES enum_types(id),
				verifier            UUID REFERENCES enum_types(id),
				created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)
		`},
		{"create credential_requests indexes", `
			CREATE INDEX IF NOT EXISTS idx_credential_requests_request_id ON credential_requests(request_id);
			CREATE INDEX IF NOT EXISTS idx_credential_requests_status ON credential_requests(verification_status);
		`},
		{"create credentials", `
			CREATE TABLE IF NOT EXISTS credentials (
				id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
				cred_id          TEXT NOT NULL,
				cred_type        UUID NOT NULL REFERENCES enum_types(id),
				cred_data        JSONB NOT NULL,
				evidences        JSONB,
				issuer           UUID REFERENCES enum_types(id),
				verifier         UUID REFERENCES enum_types(id),
				valid_from       TIMESTAMPTZ,
				valid_until      TIMESTAMPTZ,
				last_verified_at TIMESTAMPTZ,
				created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)
		`},
		{"create credentials indexes", `
			CREATE INDEX IF NOT EXISTS idx_credentials_cred_id ON credentials(cred_id);
			CREATE INDEX IF NOT EXISTS idx_credentials_cred_type ON credentials(cred_type);
		`},
		{"remove stale CRED_PAN/CRED_GST enum values", `
			DELETE FROM enum_types WHERE category = 'CRED_TYPE' AND value IN ('CRED_PAN', 'CRED_GST')
		`},
		{"add dedup columns to credential_requests", `
			ALTER TABLE credential_requests ADD COLUMN IF NOT EXISTS participant_id TEXT NOT NULL DEFAULT '';
			ALTER TABLE credential_requests ADD COLUMN IF NOT EXISTS payload_hash TEXT NOT NULL DEFAULT '';
			CREATE INDEX IF NOT EXISTS idx_credential_requests_dedup
				ON credential_requests(participant_id, payload_hash, verification_status);
		`},
		{"convert issuer to enum FK", `
			DO $$
			BEGIN
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_name = 'credential_requests' AND column_name = 'issuer' AND data_type <> 'uuid'
				) THEN
					ALTER TABLE credential_requests ALTER COLUMN issuer DROP DEFAULT;
					ALTER TABLE credential_requests ALTER COLUMN issuer DROP NOT NULL;
					ALTER TABLE credential_requests ALTER COLUMN issuer TYPE UUID USING NULLIF(issuer, '')::uuid;
				END IF;
				IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'credential_requests_issuer_fkey') THEN
					ALTER TABLE credential_requests ADD CONSTRAINT credential_requests_issuer_fkey
						FOREIGN KEY (issuer) REFERENCES enum_types(id);
				END IF;

				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_name = 'credentials' AND column_name = 'issuer' AND data_type <> 'uuid'
				) THEN
					ALTER TABLE credentials ALTER COLUMN issuer DROP DEFAULT;
					ALTER TABLE credentials ALTER COLUMN issuer DROP NOT NULL;
					ALTER TABLE credentials ALTER COLUMN issuer TYPE UUID USING NULLIF(issuer, '')::uuid;
				END IF;
				IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'credentials_issuer_fkey') THEN
					ALTER TABLE credentials ADD CONSTRAINT credentials_issuer_fkey
						FOREIGN KEY (issuer) REFERENCES enum_types(id);
				END IF;
			END $$;
		`},
	}

	for _, m := range migrations {
		if err := db.Exec(m.sql).Error; err != nil {
			return fmt.Errorf("migration %q failed: %w", m.name, err)
		}
		log.Printf("migration %q applied", m.name)
	}

	if err := seedEnumTypes(db); err != nil {
		return fmt.Errorf("seeding enum types failed: %w", err)
	}

	log.Println("database migrations complete")
	return nil
}

func seedEnumTypes(db *gorm.DB) error {
	panSchema := `{"properties":{"id_no":"string","name":"string","dob":"string"},"required":["id_no","name","dob"]}`
	gstSchema := `{"properties":{"id_no":"string"},"required":["id_no"]}`
	fssaiSchema := `{"properties":{"id_no":"string"},"required":["id_no"]}`
	udyamSchema := `{"properties":{"id_no":"string"},"required":["id_no"]}`

	seeds := []struct {
		category    string
		value       string
		label       string
		jsonSchema  *string
		description string
	}{
		{"CRED_TYPE", "PAN", "PAN", &panSchema, "PAN Number"},
		{"CRED_TYPE", "GST", "GST", &gstSchema, "GST Number"},
		{"CRED_TYPE", "FSSAI", "FSSAI", &fssaiSchema, "FSSAI License Number"},
		{"CRED_TYPE", "UDYAM", "UDYAM", &udyamSchema, "Udyam Registration Number"},
		{"CRED_VERIFICATION", "PENDING", "Pending", nil, ""},
		{"CRED_VERIFICATION", "VERIFIED", "Verified", nil, ""},
		{"CRED_VERIFICATION", "FAILED", "Failed", nil, ""},
		{"CRED_VERIFICATION", "REJECTED", "Rejected", nil, ""},
		{"CRED_VERIFIER", "DIGIO", "Digio", nil, ""},
		{"CRED_VERIFIER", "MOCK", "Mock", nil, ""},
		{"CRED_ISSUER", "INCOME_TAX_DEPT", "Income Tax Department", nil, "Issuing authority for PAN"},
		{"CRED_ISSUER", "GSTN", "GST Network", nil, "Issuing authority for GST"},
		{"CRED_ISSUER", "FSSAI_AUTHORITY", "Food Safety and Standards Authority of India", nil, "Issuing authority for FSSAI"},
		{"CRED_ISSUER", "MSME", "Ministry of Micro, Small and Medium Enterprises", nil, "Issuing authority for Udyam"},
	}

	for _, s := range seeds {
		sql := `INSERT INTO enum_types (category, value, label, json_schema, description)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (category, value) DO UPDATE SET
				label = EXCLUDED.label,
				json_schema = EXCLUDED.json_schema,
				description = EXCLUDED.description`
		if err := db.Exec(sql, s.category, s.value, s.label, s.jsonSchema, s.description).Error; err != nil {
			return fmt.Errorf("seed %s/%s failed: %w", s.category, s.value, err)
		}
	}

	log.Println("enum types seeded")
	return nil
}
