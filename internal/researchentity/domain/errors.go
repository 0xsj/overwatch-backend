package domain

import "github.com/0xsj/overwatch-backend/pkg/errors"

var (
	ErrIDRequired               = errors.New(errors.Invalid, "an identifier is required")
	ErrWorkspaceRequired        = errors.New(errors.Invalid, "a research record belongs to an investigation")
	ErrTimeRequired             = errors.New(errors.Invalid, "a research record needs a timestamp")
	ErrKindUnknown              = errors.New(errors.Invalid, "research record kind must be person, account, organisation, or place")
	ErrSearchTooLong            = errors.New(errors.Invalid, "research record search must be 200 characters or fewer")
	ErrCitationFilterUnknown    = errors.New(errors.Invalid, "research record citation filter must be cited or uncited")
	ErrResolutionFilterUnknown  = errors.New(errors.Invalid, "research record resolution filter must be open, accepted, or none")
	ErrNameRequired             = errors.New(errors.Invalid, "a research record needs a name")
	ErrNameTooLong              = errors.New(errors.Invalid, "that research record name is too long")
	ErrDescriptionTooLong       = errors.New(errors.Invalid, "that research record description is too long")
	ErrObservationTooMany       = errors.New(errors.Invalid, "a research record can cite at most twelve observations")
	ErrDuplicateObservation     = errors.New(errors.Invalid, "a research record cannot cite the same observation twice")
	ErrPlacePrecisionUnknown    = errors.New(errors.Invalid, "place precision must be exact, approximate, or region")
	ErrPlaceGeometryKind        = errors.New(errors.Invalid, "place geometry can only be attached to a place record")
	ErrPlaceGeometryCoordinates = errors.New(errors.Invalid, "place coordinates must be finite latitude and longitude values")
	ErrPlaceGeometryEvidence    = errors.New(errors.Invalid, "place geometry needs up to eight cited observations already attached to the record")
	ErrNotFound                 = errors.New(errors.NotFound, "research record")
)
