// Package domain holds the profile module's entities, errors and the interface its
// usecase depends on. Standard library only.
package domain

import (
	"context"
	"errors"
	"time"
)

// Profile is the part of a research profile a user edits (FR-4.1). FullName is the one
// place the user's name is stored; the auth module reads it from the same row. An
// optional field is nil when not set.
type Profile struct {
	UserID      string
	FullName    string
	Headline    *string
	Bio         *string
	Institution *string
	Department  *string
	JobTitle    *string
	Country     *string // ISO 3166-1 alpha-2 code in capitals
	City        *string
	// PhotoFileID and CoverFileID are the stored files of the profile photo and cover
	// image (FR-4.6), nil when not set. The media module sets them; UpsertProfile leaves
	// them alone.
	PhotoFileID *string
	CoverFileID *string
	// Interests and Skills are the user's tags sorted by name (FR-4.2), Education the
	// user's education entries (FR-4.4), Experience their work experience (FR-4.8) and
	// Languages the languages they know (FR-4.9), all in profile order. UpsertProfile
	// ignores them; the usecase fills them when it reads a profile.
	Interests  []Tag
	Skills     []Tag
	Education  []Education
	Experience []Experience
	Languages  []Language
	// Contact is the contact info the viewer of the profile may see (FR-4.7). The
	// usecase fills it when it reads a profile.
	Contact VisibleContact
	// PublicationCount is how many publications the user has confirmed (FR-5.5). The
	// usecase fills it when it reads a profile.
	PublicationCount int
}

// Visibility says who may see a contact item (FR-4.7). Slice 13 adds "connections only".
type Visibility string

// Visibilities.
const (
	OnlyMe   Visibility = "only_me"   // the owner only; the default
	LoggedIn Visibility = "logged_in" // every logged-in user
)

// Birthday is a day of the year without a year (FR-4.7).
type Birthday struct {
	Month int
	Day   int
}

// Contact is a user's contact info with who may see each item (FR-4.7). Email is the
// account's address; the profile module stores only its visibility. A nil Phone or
// Birthday is not set.
type Contact struct {
	UserID             string
	Email              string
	EmailVisibility    Visibility
	Phone              *string // E.164, such as +8801712345678
	PhoneVisibility    Visibility
	Birthday           *Birthday
	BirthdayVisibility Visibility
	Links              []Link
}

// VisibleContact is the part of a user's contact info one viewer may see. A nil item is
// hidden from the viewer or not set; the response leaves it out. Links are visible to
// every logged-in user.
type VisibleContact struct {
	Email    *string
	Phone    *string
	Birthday *Birthday
	Links    []Link
}

// LinkKind says what a profile link points to.
type LinkKind string

// Link kinds.
const (
	LinkWebsite       LinkKind = "website"
	LinkGoogleScholar LinkKind = "google_scholar"
	LinkResearchGate  LinkKind = "researchgate"
	LinkLinkedIn      LinkKind = "linkedin"
	LinkGitHub        LinkKind = "github"
	LinkOther         LinkKind = "other"
)

// Link is a link on a user's profile (FR-4.7).
type Link struct {
	ID     string
	UserID string
	Kind   LinkKind
	URL    string // https only
}

// Education is one entry of a user's education (FR-4.4). An optional field is nil when
// not set; a nil EndYear means the entry is current.
type Education struct {
	ID           string
	UserID       string
	Institution  string
	Degree       *string
	FieldOfStudy *string
	StartYear    int
	EndYear      *int
	Grade        *string // free text, such as "CGPA 3.57 / 4.00"
	Description  *string
}

// YearMonth is a month of a year; Month runs from 1 to 12.
type YearMonth struct {
	Year  int
	Month int
}

// Before reports whether m is an earlier month than o.
func (m YearMonth) Before(o YearMonth) bool {
	return m.Year < o.Year || (m.Year == o.Year && m.Month < o.Month)
}

// EmploymentType says how a user was employed in a role (FR-4.8).
type EmploymentType string

// Employment types.
const (
	EmploymentFullTime          EmploymentType = "full_time"
	EmploymentPartTime          EmploymentType = "part_time"
	EmploymentInternship        EmploymentType = "internship"
	EmploymentContract          EmploymentType = "contract"
	EmploymentFreelance         EmploymentType = "freelance"
	EmploymentSelfEmployed      EmploymentType = "self_employed"
	EmploymentResearchAssistant EmploymentType = "research_assistant"
	EmploymentTeachingAssistant EmploymentType = "teaching_assistant"
	EmploymentPostdoc           EmploymentType = "postdoc"
	EmploymentVolunteer         EmploymentType = "volunteer"
)

// LocationType says where a role was done (FR-4.8).
type LocationType string

// Location types.
const (
	LocationOnSite LocationType = "on_site"
	LocationHybrid LocationType = "hybrid"
	LocationRemote LocationType = "remote"
)

// Experience is one entry of a user's work experience (FR-4.8). An optional field is nil
// when not set. End is nil exactly when IsCurrent is true.
type Experience struct {
	ID             string
	UserID         string
	Title          string
	Organization   string
	EmploymentType EmploymentType
	Location       *string
	LocationType   *LocationType
	Start          YearMonth
	End            *YearMonth
	IsCurrent      bool
	Description    *string
}

// Proficiency says how well a user knows a language (FR-4.9).
type Proficiency string

// Proficiency levels, lowest first.
const (
	ProficiencyElementary          Proficiency = "elementary"
	ProficiencyLimitedWorking      Proficiency = "limited_working"
	ProficiencyProfessionalWorking Proficiency = "professional_working"
	ProficiencyFullProfessional    Proficiency = "full_professional"
	ProficiencyNativeOrBilingual   Proficiency = "native_or_bilingual"
)

// Language is one language a user knows (FR-4.9). Name keeps the casing the user typed;
// a user has no two languages whose names differ only in letter case.
type Language struct {
	ID          string
	UserID      string
	Name        string
	Proficiency Proficiency
}

// PublicationSource says how a publication was added (FR-5.1, FR-5.2, FR-5.3).
type PublicationSource string

// Publication sources.
const (
	SourceManual PublicationSource = "manual" // typed in by hand
	SourceDOI    PublicationSource = "doi"    // fetched for a DOI (slice 11.2)
	SourceORCID  PublicationSource = "orcid"  // imported from ORCID (slice 11.3)
)

// Publication is a publication record (FR-5.1). A record with a DOI is stored once and
// linked to every author who is a user (FR-5.4). An optional field is nil when not set;
// DOI is the bare, lower-case form, such as 10.1145/3292500.3330701. Year is set for every
// manual record; an imported one may have none.
type Publication struct {
	ID        string
	DOI       *string
	Title     string
	Year      *int
	Venue     *string
	Abstract  *string
	URL       *string
	Source    PublicationSource
	CreatedBy *string // the user who added the record; nil once that account is deleted
	CreatedAt time.Time
	UpdatedAt time.Time
	// AddedAt is when the user whose list it is linked to the record. Only
	// ListConfirmedPublications fills it.
	AddedAt time.Time
}

// PublicationPage is one page of a user's confirmed publications. Total counts them on
// all pages.
type PublicationPage struct {
	Items    []Publication
	Page     int
	PageSize int
	Total    int
}

// LinkState says how a user is linked to a publication.
type LinkState int

// Link states.
const (
	NotLinked   LinkState = iota
	Unconfirmed           // linked, not yet confirmed by the user (FR-5.5)
	Confirmed
)

// TagKind says which shared vocabulary a tag belongs to.
type TagKind string

// Tag kinds.
const (
	KindInterest TagKind = "interest" // research_interests, user_interests
	KindSkill    TagKind = "skill"    // skills, user_skills
)

// Tag is a research interest or a skill (FR-4.2). Tags are shared by all users; names
// that differ only in letter case are the same tag, and Name keeps the spelling the tag
// was first created with.
type Tag struct {
	ID   string
	Name string
}

// Account is a user as the profile module sees it: whether others may see the profile,
// and the profile row when there is one.
type Account struct {
	UserID    string
	Verified  bool // the email address is verified (FR-1.4)
	Suspended bool // the account is suspended (FR-3.4)
	// Profile is nil when the user has no profile row.
	Profile *Profile
}

// Store reads and writes profiles, the user's tags, education and experience entries,
// languages, contact info and links. Writes that must happen together go through RunInTx.
type Store interface {
	// RunInTx runs fn in one database transaction. It commits when fn returns nil and
	// rolls back otherwise, returning fn's error.
	RunInTx(ctx context.Context, fn func(tx Tx) error) error
	// FindAccount returns ErrNotFound when no user has the ID, or the ID is not a UUID.
	FindAccount(ctx context.Context, userID string) (Account, error)
	// UpsertProfile creates the user's profile row or replaces its edited fields, and
	// returns what was stored. It returns ErrNotFound when the user does not exist.
	UpsertProfile(ctx context.Context, p Profile) (Profile, error)
	// ListTags returns the user's tags of kind, sorted by name ignoring letter case.
	ListTags(ctx context.Context, userID string, kind TagKind) ([]Tag, error)
	// RemoveTag unlinks the tag from the user; the tag itself stays. It returns
	// ErrNotFound when the user does not have the tag, or tagID is not a UUID.
	RemoveTag(ctx context.Context, userID string, kind TagKind, tagID string) error
	// SuggestTags returns at most limit tags of kind whose name contains q, ignoring
	// letter case (FR-4.3): names that start with q first, then by how many users have
	// the tag (most first), then by name.
	SuggestTags(ctx context.Context, kind TagKind, q string, limit int) ([]Tag, error)
	// ListEducation returns the user's education entries: current entries first, then by
	// end year, latest first, then by start year, latest first.
	ListEducation(ctx context.Context, userID string) ([]Education, error)
	// DeleteEducation removes the user's entry. It returns ErrNotFound when the user has
	// no entry with the ID, including one that belongs to someone else, or the ID is not
	// a UUID.
	DeleteEducation(ctx context.Context, userID, id string) error
	// ListExperience returns the user's experience entries: current entries first, then
	// by start, latest first, then the oldest entry first.
	ListExperience(ctx context.Context, userID string) ([]Experience, error)
	// DeleteExperience removes the user's entry. It returns ErrNotFound when the user has
	// no entry with the ID, including one that belongs to someone else, or the ID is not
	// a UUID.
	DeleteExperience(ctx context.Context, userID, id string) error
	// ListLanguages returns the user's languages, oldest first.
	ListLanguages(ctx context.Context, userID string) ([]Language, error)
	// DeleteLanguage removes the user's language. It returns ErrNotFound when the user has
	// no language with the ID, including one that belongs to someone else, or the ID is
	// not a UUID.
	DeleteLanguage(ctx context.Context, userID, id string) error
	// FindContact returns the user's contact info without links. A user who never saved
	// any gets no phone, no birthday and every visibility OnlyMe. It returns ErrNotFound
	// when the user does not exist.
	FindContact(ctx context.Context, userID string) (Contact, error)
	// UpsertContact creates the user's contact row or replaces its phone, birthday and
	// visibilities. It returns ErrNotFound when the user does not exist.
	UpsertContact(ctx context.Context, c Contact) error
	// ListLinks returns the user's links, oldest first.
	ListLinks(ctx context.Context, userID string) ([]Link, error)
	// DeleteLink removes the user's link. It returns ErrNotFound when the user has no
	// link with the ID, including one that belongs to someone else, or the ID is not a
	// UUID.
	DeleteLink(ctx context.Context, userID, id string) error
	// ListConfirmedPublications returns every publication the user has confirmed, with
	// AddedAt set, in no particular order. A user has at most a few hundred.
	ListConfirmedPublications(ctx context.Context, userID string) ([]Publication, error)
	// CountConfirmedPublications returns how many publications the user has confirmed.
	CountConfirmedPublications(ctx context.Context, userID string) (int, error)
}

// Tx is the set of steps available inside a transaction.
type Tx interface {
	// LockUser locks the user against other tag, education, experience, language, link and
	// publication changes until the transaction ends, so two requests cannot both pass a limit. It
	// returns ErrNotFound when the user does not exist.
	LockUser(ctx context.Context, userID string) error
	// FindOrCreateTag returns the tag of kind named name ignoring letter case, creating
	// it with this spelling when there is none. A tag created at the same moment by
	// another transaction is found, not duplicated.
	FindOrCreateTag(ctx context.Context, kind TagKind, name string) (Tag, error)
	// HasTag reports whether the user has the tag.
	HasTag(ctx context.Context, userID string, kind TagKind, tagID string) (bool, error)
	// CountTags returns how many tags of kind the user has.
	CountTags(ctx context.Context, userID string, kind TagKind) (int, error)
	// LinkTag adds the tag to the user's profile.
	LinkTag(ctx context.Context, userID string, kind TagKind, tagID string) error
	// CountEducation returns how many education entries the user has.
	CountEducation(ctx context.Context, userID string) (int, error)
	// InsertEducation stores a new entry for e.UserID and returns it with its ID.
	InsertEducation(ctx context.Context, e Education) (Education, error)
	// FindEducationForUpdate returns the user's entry and locks it until the transaction
	// ends. It returns ErrNotFound when the user has no entry with the ID, including one
	// that belongs to someone else, or the ID is not a UUID.
	FindEducationForUpdate(ctx context.Context, userID, id string) (Education, error)
	// UpdateEducation replaces the fields of the entry e.ID that belongs to e.UserID and
	// returns what was stored. It returns ErrNotFound when there is no such entry.
	UpdateEducation(ctx context.Context, e Education) (Education, error)
	// CountExperience returns how many experience entries the user has.
	CountExperience(ctx context.Context, userID string) (int, error)
	// InsertExperience stores a new entry for e.UserID and returns it with its ID.
	InsertExperience(ctx context.Context, e Experience) (Experience, error)
	// FindExperienceForUpdate returns the user's entry and locks it until the transaction
	// ends. It returns ErrNotFound when the user has no entry with the ID, including one
	// that belongs to someone else, or the ID is not a UUID.
	FindExperienceForUpdate(ctx context.Context, userID, id string) (Experience, error)
	// UpdateExperience replaces the fields of the entry e.ID that belongs to e.UserID and
	// returns what was stored. It returns ErrNotFound when there is no such entry.
	UpdateExperience(ctx context.Context, e Experience) (Experience, error)
	// CountLanguages returns how many languages the user has.
	CountLanguages(ctx context.Context, userID string) (int, error)
	// LanguageNameTaken reports whether the user has a language named name, ignoring
	// letter case, other than the one with ID exceptID ("" excepts none).
	LanguageNameTaken(ctx context.Context, userID, name, exceptID string) (bool, error)
	// InsertLanguage stores a new language for l.UserID and returns it with its ID. It
	// returns ErrLanguageExists when the user has the name already, ignoring letter case.
	InsertLanguage(ctx context.Context, l Language) (Language, error)
	// FindLanguageForUpdate returns the user's language and locks it until the
	// transaction ends. It returns ErrNotFound when the user has no language with the ID,
	// including one that belongs to someone else, or the ID is not a UUID.
	FindLanguageForUpdate(ctx context.Context, userID, id string) (Language, error)
	// UpdateLanguage replaces the fields of the language l.ID that belongs to l.UserID and
	// returns what was stored. It returns ErrNotFound when there is no such language and
	// ErrLanguageExists when another of the user's languages has the name.
	UpdateLanguage(ctx context.Context, l Language) (Language, error)
	// CountLinks returns how many links the user has.
	CountLinks(ctx context.Context, userID string) (int, error)
	// InsertLink stores a new link for l.UserID and returns it with its ID.
	InsertLink(ctx context.Context, l Link) (Link, error)
	// CountPublicationLinks returns how many publications the user is linked to,
	// confirmed or not.
	CountPublicationLinks(ctx context.Context, userID string) (int, error)
	// FindPublicationByDOI returns the publication with the DOI and keeps it from being
	// deleted until the transaction ends. It returns ErrNotFound when there is none.
	FindPublicationByDOI(ctx context.Context, doi string) (Publication, error)
	// InsertPublication stores a new publication and returns it with its ID. It returns
	// ErrDuplicateDOI, and stores nothing, when another publication has the DOI, including
	// one stored at the same moment by another transaction.
	InsertPublication(ctx context.Context, p Publication) (Publication, error)
	// PublicationLink returns how the user is linked to the publication.
	PublicationLink(ctx context.Context, userID, publicationID string) (LinkState, error)
	// LinkPublication links the user to the publication, confirmed.
	LinkPublication(ctx context.Context, userID, publicationID string) error
	// ConfirmPublication confirms the user's link to the publication.
	ConfirmPublication(ctx context.Context, userID, publicationID string) error
	// FindPublicationForUpdate returns the publication and locks it until the transaction
	// ends. It returns ErrNotFound when the user is not linked to it, or the ID is not a
	// UUID.
	FindPublicationForUpdate(ctx context.Context, userID, id string) (Publication, error)
	// CountPublicationAuthors returns how many users are linked to the publication,
	// confirmed or not.
	CountPublicationAuthors(ctx context.Context, publicationID string) (int, error)
	// UpdatePublication replaces the edited fields of publication p.ID and returns what
	// was stored. It returns ErrDuplicateDOI when another publication has the DOI.
	UpdatePublication(ctx context.Context, p Publication) (Publication, error)
	// UnlinkPublication removes the user's link to the publication. It returns
	// ErrNotFound when there is none.
	UnlinkPublication(ctx context.Context, userID, publicationID string) error
	// DeletePublicationIfOrphaned deletes the publication when no user is linked to it.
	DeletePublicationIfOrphaned(ctx context.Context, publicationID string) error
}

// Errors the usecase returns. The HTTP adapter turns each into an error code.
var (
	ErrNotFound           = errors.New("profile not found")
	ErrInvalidFullName    = errors.New("full name is empty, too long or has a line break")
	ErrInvalidHeadline    = errors.New("headline is too long or has a line break")
	ErrInvalidBio         = errors.New("bio is too long")
	ErrInvalidInstitution = errors.New("institution is too long or has a line break")
	ErrInvalidDepartment  = errors.New("department is too long or has a line break")
	ErrInvalidJobTitle    = errors.New("job title is too long or has a line break")
	ErrInvalidCountry     = errors.New("country is not an ISO 3166-1 alpha-2 code")
	ErrInvalidCity        = errors.New("city is too long or has a line break")
	ErrInvalidTagName     = errors.New("tag name is empty or too long")
	ErrTagLimitReached    = errors.New("user has the most tags of this kind allowed")
	ErrInvalidLimit       = errors.New("limit is less than 1")

	ErrInvalidEducationInstitution = errors.New("education institution is empty, too long or has a line break")
	ErrInvalidDegree               = errors.New("degree is too long or has a line break")
	ErrInvalidFieldOfStudy         = errors.New("field of study is too long or has a line break")
	ErrInvalidStartYear            = errors.New("start year is missing or out of range")
	ErrInvalidEndYear              = errors.New("end year is out of range or before the start year")
	ErrInvalidGrade                = errors.New("grade is too long or has a line break")
	ErrEducationLimitReached       = errors.New("user has the most education entries allowed")

	ErrInvalidTitle           = errors.New("title is empty, too long or has a line break")
	ErrInvalidOrganization    = errors.New("organization is empty, too long or has a line break")
	ErrInvalidEmploymentType  = errors.New("employment type is not one of the allowed types")
	ErrInvalidLocation        = errors.New("location is too long or has a line break")
	ErrInvalidLocationType    = errors.New("location type is not one of the allowed types")
	ErrInvalidStartDate       = errors.New("start date is missing, not a real month, before 1950 or in the future")
	ErrInvalidEndDate         = errors.New("end date is not a real month, before 1950 or in the future")
	ErrInvalidIsCurrent       = errors.New("isCurrent is missing")
	ErrEndDateRequired        = errors.New("an entry that is not current needs an end date")
	ErrEndDateNotAllowed      = errors.New("a current entry has no end date")
	ErrInvalidDateRange       = errors.New("end date is before the start date")
	ErrInvalidDescription     = errors.New("description is too long")
	ErrExperienceLimitReached = errors.New("user has the most experience entries allowed")

	ErrInvalidLanguageName  = errors.New("language name is empty, too long or has a line break")
	ErrInvalidProficiency   = errors.New("proficiency is not one of the allowed levels")
	ErrLanguageExists       = errors.New("user has a language with this name already")
	ErrLanguageLimitReached = errors.New("user has the most languages allowed")

	ErrInvalidPhone      = errors.New("phone is not in E.164 format")
	ErrInvalidBirthday   = errors.New("birthday is not a day of the year")
	ErrInvalidVisibility = errors.New("visibility is not one of the allowed values")
	ErrInvalidLinkKind   = errors.New("link kind is not one of the allowed kinds")
	ErrInvalidLinkURL    = errors.New("link URL is not an https URL of at most 300 characters")
	ErrLinkLimitReached  = errors.New("user has the most links allowed")

	ErrInvalidYear               = errors.New("year is missing or out of range")
	ErrInvalidVenue              = errors.New("venue is too long or has a line break")
	ErrInvalidAbstract           = errors.New("abstract is too long")
	ErrInvalidPublicationURL     = errors.New("publication URL is not an http or https URL of at most 500 characters")
	ErrInvalidDOI                = errors.New("DOI is not of the form 10.NNNN/suffix")
	ErrPublicationLimitReached   = errors.New("user has the most publications allowed")
	ErrPublicationAlreadyAdded   = errors.New("user has the publication with this DOI already")
	ErrPublicationSharedReadonly = errors.New("publication is shared with another user or was not added by hand")
	ErrDuplicateDOI              = errors.New("another publication has this DOI")
	ErrInvalidPage               = errors.New("page or page size is less than 1")
)
