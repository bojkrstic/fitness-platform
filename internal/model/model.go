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
}

type TrainingsPageData struct {
	BasePageData
	Trainings []Training
}

type TrainingRoomPageData struct {
	BasePageData
	Training Training
}

type AuthPageData struct {
	BasePageData
	Next  string
	Email string
}

type AdminPageData struct {
	BasePageData
	Trainings []Training
	Form      TrainingForm
}
