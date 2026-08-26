package dto_test

import (
	"testing"

	"credential-service/internal/handlers/dto"
	"credential-service/internal/utils"
)

// Proves the flat CredentialItem shape validates per-element when inside a
// dive'd slice — notably the mixed-batch case, where one invalid item must
// fail without a valid sibling masking it (or vice versa).
func TestSubmitCredentialsRequestValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		req     dto.SubmitCredentialsRequest
		wantErr bool
	}{
		{
			name: "PAN with cred_id only is valid",
			req: dto.SubmitCredentialsRequest{
				ParticipantID: "pid-1",
				Credentials: []dto.CredentialItem{
					{CredType: "PAN", CredID: "ABCDE1234F"},
				},
			},
			wantErr: false,
		},
		{
			name: "GST does not require name/dob",
			req: dto.SubmitCredentialsRequest{
				ParticipantID: "pid-1",
				Credentials: []dto.CredentialItem{
					{CredType: "GST", CredID: "29AABCU9603R1ZM"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing cred_id",
			req: dto.SubmitCredentialsRequest{
				ParticipantID: "pid-1",
				Credentials: []dto.CredentialItem{
					{CredType: "GST"},
				},
			},
			wantErr: true,
		},
		{
			name: "missing participant_id",
			req: dto.SubmitCredentialsRequest{
				Credentials: []dto.CredentialItem{
					{CredType: "GST", CredID: "29AABCU9603R1ZM"},
				},
			},
			wantErr: true,
		},
		{
			name: "mixed batch: both valid with id-only items",
			req: dto.SubmitCredentialsRequest{
				ParticipantID: "pid-1",
				Credentials: []dto.CredentialItem{
					{CredType: "GST", CredID: "29AABCU9603R1ZM"},
					{CredType: "PAN", CredID: "ABCDE1234F"},
				},
			},
			wantErr: false,
		},
		{
			name: "mixed batch: one missing cred_id fails the whole request",
			req: dto.SubmitCredentialsRequest{
				ParticipantID: "pid-1",
				Credentials: []dto.CredentialItem{
					{CredType: "GST", CredID: "29AABCU9603R1ZM"},
					{CredType: "PAN"},
				},
			},
			wantErr: true,
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
