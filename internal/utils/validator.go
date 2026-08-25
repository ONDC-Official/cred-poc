package utils

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

func Validate(s any) error {
	if err := validate.Struct(s); err != nil {
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			for _, fieldErr := range validationErrors {
				return fmt.Errorf("%s is required", jsonFieldName(fieldErr))
			}
		}
		return err
	}
	return nil
}

func jsonFieldName(fe validator.FieldError) string {
	switch fe.Field() {
	case "CredID":
		return "cred_id"
	case "CredType":
		return "cred_type"
	case "Name":
		return "name"
	case "Dob":
		return "dob"
	default:
		return fe.Field()
	}
}
