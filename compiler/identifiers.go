package compiler

import (
	"fmt"
	"unicode"
)

func validateIdentifier(kind, identifier string) error {
	if identifier == "" {
		return fmt.Errorf("%s identifier cannot be empty", kind)
	}

	for index, character := range identifier {
		if index == 0 {
			if character != '_' && !unicode.IsLetter(character) {
				return fmt.Errorf("invalid %s identifier %q", kind, identifier)
			}
			continue
		}
		if character != '_' && !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			return fmt.Errorf("invalid %s identifier %q", kind, identifier)
		}
	}
	return nil
}
