package model

type User struct {
	ID    string
	Email string
	Role  string
}

type AuthUser struct {
	User
	PasswordHash string
}

type Training struct {
	ID          string
	Title       string
	Trainer     string
	Description string
	Time        string
	CreatedBy   string
}

type LiveStats struct {
	Online   int `json:"online"`
	Watching int `json:"watching"`
}

type AdminTraining struct {
	Training
	LiveStats LiveStats
}

type Recording struct {
	ID               string
	TrainingID       string
	ObjectName       string
	OriginalFilename string
	ContentType      string
	SizeBytes        int64
	DurationSeconds  int
	RecordedAt       string
	CreatedBy        string
}

type BillingCustomer struct {
	UserID           string
	StripeCustomerID string
	CreatedAt        string
	UpdatedAt        string
}

type Subscription struct {
	ID                   string
	UserID               string
	StripeCustomerID     string
	StripeSubscriptionID string
	StripePriceID        string
	Status               string
	CurrentPeriodStart   string
	CurrentPeriodEnd     string
	CancelAtPeriodEnd    bool
	CreatedAt            string
	UpdatedAt            string
}

type TrainingForm struct {
	Title       string
	Trainer     string
	Description string
	Time        string
}

type BasePageData struct {
	CurrentUser *User
	Error       string
	Success     string
}

type HomePageData struct {
	BasePageData
	Trainings []Training
	CanAccess bool
}

type TrainingsPageData struct {
	BasePageData
	Trainings []Training
	CanAccess bool
}

type TrainingRoomPageData struct {
	BasePageData
	Training              Training
	Recordings            []Recording
	ICEServers            any
	RecordingEnabled      bool
	RecordingDisabled     string
	RecordingStorageMode  string
	CanAccess             bool
	BillingEnabled        bool
	BillingDisabledReason string
	Subscription          *Subscription
	ReturnTo              string
	SuccessMessage        string
}

type AuthPageData struct {
	BasePageData
	Next  string
	Email string
}

type BillingPageData struct {
	BasePageData
	BillingEnabled        bool
	BillingDisabledReason string
	Subscription          *Subscription
	ReturnTo              string
}

type AdminPageData struct {
	BasePageData
	Trainings             []AdminTraining
	Form                  TrainingForm
	BillingEnabled        bool
	BillingDisabledReason string
	StripePriceID         string
}
