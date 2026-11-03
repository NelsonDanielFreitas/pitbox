package domain

// Limits of a sheet template. They bound the work the validators do on input
// that comes from a file in the repository today and from a team editor later.
const (
	MaxTemplateGroups   = 40
	MaxTemplateFields   = 400
	MaxChoiceOptions    = 20
	MinChoiceOptions    = 2
	MaxOptionLen        = 40
	MinTableColumns     = 2
	MaxTableColumns     = 4
	MaxTableRows        = 30
	MaxLabelLen         = 80
	MaxHelpLen          = 300
	MaxPartRefLen       = 40
	MaxTextFieldLen     = 500 // the plan said 200, but v1 diffs.fill_notes allows 500
	MaxTemplateBytes    = 256 << 10
	templateFormat      = 1
	maxTemplateNameLen  = 120
	maxMakeModelLen     = 80
	maxSourceURLLen     = 500
	maxUnitLen          = 20
	maxSheetRevisionLen = 40
	maxIntegerMagnitude = 1 << 53 // largest range an int field may use without float64 rounding
)
