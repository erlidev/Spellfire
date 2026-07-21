package model

type Class string

const (
	Gunslinger Class = "gunslinger"
	Mage       Class = "mage"
)

func (c Class) Valid() bool { return c == Gunslinger || c == Mage }

type Account struct {
	ID           string
	Email        string
	PasswordHash []byte
}

type Character struct {
	ID        string `json:"id"`
	AccountID string `json:"-"`
	Name      string `json:"name"`
	Class     Class  `json:"class"`
	Level     int    `json:"level"`
	XP        int    `json:"xp"`
}
