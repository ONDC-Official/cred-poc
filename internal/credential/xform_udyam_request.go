package credential

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var (
	udyamHyphenatedPattern = regexp.MustCompile(`^UDYAM-[A-Z]{2}-\d{2}-\d{7}$`)
	udyamCompactPattern    = regexp.MustCompile(`^UDYAM[A-Z]{2}\d{2}\d{7}$`)
)

func udyamCanonicalize(_ json.RawMessage, normalizedID string) (any, error) {
	return map[string]string{"id_no": toCanonicalUdyam(normalizedID)}, nil
}

func toCanonicalUdyam(raw string) string {
	normalized := strings.ToUpper(strings.TrimSpace(raw))
	if udyamHyphenatedPattern.MatchString(normalized) {
		return normalized
	}
	compact := strings.ReplaceAll(strings.ReplaceAll(normalized, "-", ""), " ", "")
	if udyamCompactPattern.MatchString(compact) {
		return fmt.Sprintf("%s-%s-%s-%s", compact[0:5], compact[5:7], compact[7:9], compact[9:16])
	}
	return normalized
}
