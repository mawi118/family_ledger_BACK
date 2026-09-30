package proto

import (
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		pass string
		want string // пустая строка — пароль валиден
	}{
		{"Testpass123!", ""},
		{"Abcdef1!", ""},                               // ровно 8
		{"Abcdefghijklmnopqrstu1!x!", ""},              // 25
		{"Ab1!", msgPasswordLength},                    // короткий
		{strings.Repeat("aB1!", 7), msgPasswordLength}, // 28 символов
		{"12345678!", msgPasswordNoLetter},
		{"Password!!", msgPasswordNoDigit},
		{"Password12", msgPasswordNoSpec},
		{"Pass qwerty!1", msgPasswordSequence}, // qwerty
		{"Xx123456!z", msgPasswordSequence},    // 123456
		{"Xx!65432z1", msgPasswordSequence},    // 6543 2 в обратном порядке: 65432 (5 подряд)
		{"Xx!QWERTz1", msgPasswordSequence},    // регистр не важен
		{"Xx!qwer1zz", ""},                     // 4 подряд — ещё можно
		{"Xx!1234zz9", ""},                     // 4 подряд — ещё можно
	}
	for _, c := range cases {
		err := validatePassword(c.pass)
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != c.want {
			t.Errorf("validatePassword(%q): ожидали %q, получили %q", c.pass, c.want, got)
		}
	}
}

func TestValidateFirstName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Иван", ""},
		{"Мария-Антуанетта", ""},
		{"Анна Мария", ""},
		{"Jean", ""},
		{"Ян", ""},
		{"Я", msgNameTooShort},
		{"", msgNameTooShort},
		{strings.Repeat("а", 100), ""},
		{strings.Repeat("а", 101), msgNameTooLong},
		{"--", msgNameChars},
		{"  ", msgNameChars},
		{" Иван", msgNameChars},
		{"Иван ", msgNameChars},
		{"Иван-", msgNameChars},
		{"Иван  Петров", msgNameChars},
		{"Иван1", msgNameChars},
		{"Иван!", msgNameChars},
	}
	for _, c := range cases {
		err := validateFirstName(c.name)
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != c.want {
			t.Errorf("validateFirstName(%q): ожидали %q, получили %q", c.name, c.want, got)
		}
	}
}

func TestValidateEmail(t *testing.T) {
	long := strings.Repeat("a", 250) + "@x.ru" // 255 символов
	cases := map[string]bool{
		"user@example.com":  true,
		"a.b+c@ex-ample.ru": true,
		"user@example":      false,
		"@example.com":      false,
		"user example.com":  false,
		"":                  false,
		long:                false,
	}
	for email, ok := range cases {
		if err := validateEmail(email); (err == nil) != ok {
			t.Errorf("validateEmail(%q): валидность %v, ошибка %v", email, ok, err)
		}
	}
}

func TestNormalize(t *testing.T) {
	r := &RegisterRequest{Email: "  Foo@Example.COM ", FirstName: "  иван-петров ", Password: " Pass1! "}
	r.Normalize()
	if r.Email != "foo@example.com" {
		t.Errorf("email: %q", r.Email)
	}
	if r.FirstName != "Иван-петров" {
		t.Errorf("имя: %q", r.FirstName)
	}
	if r.Password != " Pass1! " {
		t.Errorf("пароль не должен меняться: %q", r.Password)
	}

	l := &LoginRequest{Email: " USER@x.ru "}
	l.Normalize()
	if l.Email != "user@x.ru" {
		t.Errorf("login email: %q", l.Email)
	}
}
