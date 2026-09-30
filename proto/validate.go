package proto

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxEmailLen = 254
	minNameLen  = 2
	maxNameLen  = 100
	minPassLen  = 8
	maxPassLen  = 26
)

var (
	emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	// Только буквы, разделённые одиночными пробелами или дефисами: без пустых имён, "--", "  ",
	// пробелов и дефисов по краям. Пример допустимого: Мария-Антуанетта.
	nameRegex = regexp.MustCompile(`^\p{L}+(?:[ -]\p{L}+)*$`)
)

const passwordSpecialChars = `,."'*?!@#$%^&()-+=~`

// Тексты ошибок — из таблицы требований (док 1) без изменений, включая формулировки.
const (
	msgPasswordLength   = "Пароль должен быть длинной от 8 до 26 символов включительно"
	msgPasswordNoLetter = "Пароль должен иметь хоть одну букву"
	msgPasswordNoDigit  = "Пароль должен иметь хоть одну цифру"
	msgPasswordNoSpec   = "Пароль должен иметь хоть один специальный символ: , . ' \" * ? ! @ # $ % ^ & ( ) - + = ~"
	msgPasswordSequence = "Пароль не должен содержать подряд идущие буквы или цифры, например qwerty"
	msgEmailFormat      = "Неверный формат почты"
	msgNameChars        = "Имя должно содержать только буквы, пробел и дефис"
	msgNameTooShort     = "Имя должно быть длиной от 2 символов"
	msgNameTooLong      = "Имя слишком длинное"
)

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
	runes := []rune(strings.ToLower(password))
	windowLen := maxRun + 1
	for i := 0; i+windowLen <= len(runes); i++ {
		window := string(runes[i : i+windowLen])
		for _, seq := range keyboardSequences {
			if strings.Contains(seq, window) {
				return true
			}
		}
	}
	return false
}

func validatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if length < minPassLen || length > maxPassLen {
		return errors.New(msgPasswordLength)
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
		return errors.New(msgPasswordNoLetter)
	}
	if !hasDigit {
		return errors.New(msgPasswordNoDigit)
	}
	if !hasSpecial {
		return errors.New(msgPasswordNoSpec)
	}
	if hasKeyboardSequence(password, 4) {
		return errors.New(msgPasswordSequence)
	}
	return nil
}

func validateEmail(email string) error {
	if len(email) > maxEmailLen || !emailRegex.MatchString(email) {
		return errors.New(msgEmailFormat)
	}
	return nil
}

func validateFirstName(name string) error {
	length := utf8.RuneCountInString(name)
	if length < minNameLen {
		return errors.New(msgNameTooShort)
	}
	if length > maxNameLen {
		return errors.New(msgNameTooLong)
	}
	if !nameRegex.MatchString(name) {
		return errors.New(msgNameChars)
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// normalizeName убирает пробелы по краям и делает первую букву заглавной.
func normalizeName(name string) string {
	name = strings.TrimSpace(name)
	r, size := utf8.DecodeRuneInString(name)
	if r == utf8.RuneError {
		return name
	}
	return string(unicode.ToUpper(r)) + name[size:]
}

// Normalize вызывается интерцептором до Validate. Пароль намеренно не трогаем.
func (r *RegisterRequest) Normalize() {
	r.Email = normalizeEmail(r.Email)
	r.FirstName = normalizeName(r.FirstName)
}

func (r *LoginRequest) Normalize() {
	r.Email = normalizeEmail(r.Email)
}

func (r *EmailExistsRequest) Normalize() {
	r.Email = normalizeEmail(r.Email)
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
	return validateEmail(r.Email)
}

// Login: формат почты проверяем так же, как при регистрации (док 3); пароль — без доп. правил.
func (r *LoginRequest) Validate() error {
	if err := validateEmail(r.Email); err != nil {
		return err
	}
	if r.Password == "" {
		return errors.New("Введите пароль")
	}
	return nil
}

func (r *RefreshRequest) Validate() error {
	if r.RefreshToken == "" {
		return errors.New("refresh_token is required")
	}
	return nil
}

func (r *LogoutRequest) Validate() error {
	if r.RefreshToken == "" {
		return errors.New("refresh_token is required")
	}
	return nil
}

func (r *MeRequest) Validate() error {
	if r.AccessToken == "" {
		return errors.New("access_token is required")
	}
	return nil
}
