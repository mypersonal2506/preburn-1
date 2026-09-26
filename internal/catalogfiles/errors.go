package catalogfiles

import "errors"

var (
	// ErrInvalidDocument reports YAML that does not decode into the file's
	// schema: a syntax error, an unknown field, a duplicate key, a value of the
	// wrong YAML type, an empty document or content after the first document.
	ErrInvalidDocument = errors.New("invalid document")
	// ErrMissingField reports a required field that is absent or empty.
	ErrMissingField = errors.New("missing field")
	// ErrUnknownMeter reports a meter outside the fixed meter vocabulary.
	ErrUnknownMeter = errors.New("unknown meter")
	// ErrInvalidAttribute reports an attribute key outside the vocabulary, or a
	// value of the wrong type or outside the values its key accepts.
	ErrInvalidAttribute = errors.New("invalid attribute")
	// ErrInvalidPrice reports a unit price, unit quantity, minimum charge or
	// billing increment that does not parse or is not positive where it must be.
	ErrInvalidPrice = errors.New("invalid price")
	// ErrInvalidSourceURL reports a source URL that is not an https URL with a
	// host.
	ErrInvalidSourceURL = errors.New("invalid source url")
	// ErrInvalidVerifiedOn reports a verified_on that is not a YYYY-MM-DD date.
	ErrInvalidVerifiedOn = errors.New("invalid verified_on")
	// ErrDuplicate reports a second entry for the same model in a provider's
	// pricing file, or a second rule of one model with the same meter and
	// conditions.
	ErrDuplicate = errors.New("duplicate")
	// ErrUnknownModel reports a reference to a model that is neither curated nor
	// listed under litellm_models.
	ErrUnknownModel = errors.New("unknown model")
	// ErrModelConflict reports an alias equal to a model name, or a
	// litellm_models entry that names a curated model.
	ErrModelConflict = errors.New("model conflict")
	// ErrInvalidParameter reports a parameter mapping that breaks a rule of
	// Parameter.
	ErrInvalidParameter = errors.New("invalid parameter")
)
