package core

import "errors"

// ErrFOMInvalidModule identifies invalid supplied FOM content, independently of
// the more specific local-name diagnostic. Standard bindings expose ErrorReadingFDD.
var ErrFOMInvalidModule = errors.New("FOM contains invalid module content")
