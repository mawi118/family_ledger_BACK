package proto

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxGroupNameLen = 100
	// 10 млрд рублей в копейках: защита от переполнения при будущих суммированиях.
	maxIncomeCents = 1_000_000_000_000
)

var inviteCodeRegex = regexp.MustCompile(`^[0-9]{4}$`)

// Тексты ошибок для групп. В требованиях (док 2) формулировок нет, поэтому здесь наши.
const (
	msgGroupNameLength = "Название группы должно быть от 1 до 100 символов"
	msgIncomeNegative  = "Доход не может быть отрицательным"
	msgIncomeTooBig    = "Доход слишком большой"
	msgInviteFormat    = "Код приглашения состоит из 4 цифр"
)

func (r *CreateGroupRequest) Normalize()  { r.Name = strings.TrimSpace(r.Name) }
func (r *RenameGroupRequest) Normalize()  { r.Name = strings.TrimSpace(r.Name) }
func (r *AcceptInviteRequest) Normalize() { r.Code = strings.TrimSpace(r.Code) }

// Пустое имя при создании допустимо: сервер подставит "Группа {X+1}".
func (r *CreateGroupRequest) Validate() error {
	if utf8.RuneCountInString(r.Name) > maxGroupNameLen {
		return errors.New(msgGroupNameLength)
	}
	return nil
}

func (r *RenameGroupRequest) Validate() error {
	if n := utf8.RuneCountInString(r.Name); n < 1 || n > maxGroupNameLen {
		return errors.New(msgGroupNameLength)
	}
	return nil
}

func (r *SetMyIncomeRequest) Validate() error {
	if r.Income < 0 {
		return errors.New(msgIncomeNegative)
	}
	if r.Income > maxIncomeCents {
		return errors.New(msgIncomeTooBig)
	}
	return nil
}

func (r *AcceptInviteRequest) Validate() error {
	if !inviteCodeRegex.MatchString(r.Code) {
		return errors.New(msgInviteFormat)
	}
	return nil
}
