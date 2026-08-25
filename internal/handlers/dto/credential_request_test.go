package dto_test

import (
	"testing"

	"credential-service/internal/handlers/dto"
	"credential-service/internal/utils"
)

// Proves the flat CredentialItem shape's required_if=CredType PAN tag
// behaves correctly per-element when validated inside a dive'd slice —
// notably the mixed-batch case, where one PAN item's missing dob must fail
// validation without a GST item in the same batch masking it (or vice versa).
func TestSubmitCredentialsRequestValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		req     dto.SubmitCredentialsRequest
		wantErr bool
	}{
		{
			name: "PAN requires name and dob",
			req: dto.SubmitCredentialsRequest{
				Credentials: []dto.CredentialItem{
					{CredType: "PAN", CredID: "ABCDE1234F"},
				},
			},
			wantErr: true,
		},
		{
			name: "PAN with name and dob is valid",
			req: dto.SubmitCredentialsRequest{
				Credentials: []dto.CredentialItem{
					{CredType: "PAN", CredID: "ABCDE1234F", Name: "John Doe", Dob: "01/01/1990"},
				},
			},
			wantErr: false,
		},
		{
			name: "GST does not require name/dob",
			req: dto.SubmitCredentialsRequest{
				Credentials: []dto.CredentialItem{
					{CredType: "GST", CredID: "29AABCU9603R1ZM"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing cred_id",
			req: dto.SubmitCredentialsRequest{
				Credentials: []dto.CredentialItem{
					{CredType: "GST"},
				},
			},
			wantErr: true,
		},
		{
			name: "mixed batch: one PAN item missing dob fails the whole request",
			req: dto.SubmitCredentialsRequest{
				Credentials: []dto.CredentialItem{
					{CredType: "GST", CredID: "29AABCU9603R1ZM"},
					{CredType: "PAN", CredID: "ABCDE1234F", Name: "John Doe"},
				},
			},
			wantErr: true,
		},
		{
			name: "mixed batch: both valid",
			req: dto.SubmitCredentialsRequest{
				Credentials: []dto.CredentialItem{
					{CredType: "GST", CredID: "29AABCU9603R1ZM"},
					{CredType: "PAN", CredID: "ABCDE1234F", Name: "John Doe", Dob: "01/01/1990"},
				},
			},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := utils.Validate(&tc.req)
			if tc.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}
