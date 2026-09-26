package proto

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

var (
	emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	nameRegex  = regexp.MustCompile(`^[\p{L} \-]+$`)
)

const passwordSpecialChars = `,."'*?!@#$%^&()-+=~`

// Ряды стандартной QWERTY-раскладки (только буквы и цифры, без спецклавиш и цифровой панели)
// и их развороты — для поиска подряд идущих "клавиатурных" последовательностей типа qwerty/123456.
var keyboardSequences = buildKeyboardSequences()

func buildKeyboardSequences() []string {
	rows := []string{"1234567890", "qwertyuiop", "asdfghjkl", "zxcvbnm"}
	sequences := make([]string, 0, len(rows)*2)
	for _, row := range rows {
		sequences = append(sequences, row, reverseString(row))
	}
	return sequences
}

func reverseString(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// hasKeyboardSequence возвращает true, если в пароле есть подряд идущий (maxRun+1)-символьный
// фрагмент, целиком совпадающий с частью одного из клавиатурных рядов (в любом направлении).
func hasKeyboardSequence(password string, maxRun int) bool {
	lower := strings.ToLower(password)
	windowLen := maxRun + 1
	if len(lower) < windowLen {
		return false
	}
	for i := 0; i+windowLen <= len(lower); i++ {
		window := lower[i : i+windowLen]
		for _, seq := range keyboardSequences {
			if strings.Contains(seq, window) {
				return true
			}
		}
	}
	return false
}

func validatePassword(password string) error {
	length := len([]rune(password))
	if length < 8 || length > 26 {
		return errors.New("пароль должен быть длиной от 8 до 26 символов включительно")
	}

	var hasLetter, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		case strings.ContainsRune(passwordSpecialChars, r):
			hasSpecial = true
		}
	}
	if !hasLetter {
		return errors.New("пароль должен иметь хоть одну букву")
	}
	if !hasDigit {
		return errors.New("пароль должен иметь хоть одну цифру")
	}
	if !hasSpecial {
		return errors.New("пароль должен иметь хоть один специальный символ")
	}
	if hasKeyboardSequence(password, 4) {
		return errors.New("пароль не должен содержать подряд идущие буквы или цифры, например qwerty")
	}
	return nil
}

func validateEmail(email string) error {
	if !emailRegex.MatchString(email) {
		return errors.New("неверный формат почты")
	}
	return nil
}

func validateFirstName(name string) error {
	length := len([]rune(name))
	if length < 2 {
		return errors.New("имя должно быть длиной от 2 символов")
	}
	if length > 100 {
		return errors.New("имя слишком длинное")
	}
	if !nameRegex.MatchString(name) {
		return errors.New("имя должно содержать только буквы, пробел и дефис")
	}
	return nil
}

func (r *RegisterRequest) Validate() error {
	if err := validateEmail(r.Email); err != nil {
		return err
	}
	if err := validatePassword(r.Password); err != nil {
		return err
	}
	if err := validateFirstName(r.FirstName); err != nil {
		return err
	}
	return nil
}

func (r *EmailExistsRequest) Validate() error {
	if r.Email == "" {
		return errors.New("email is required")
	}
	return nil
}

func (r *LoginRequest) Validate() error {
	if r.Email == "" || r.Password == "" {
		return errors.New("email and password are required")
	}
	return nil
}
