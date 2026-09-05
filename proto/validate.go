package proto

import "errors"

func (r *RegisterRequest) Validate() error {
	if r.Email == "" || r.Password == "" {
		return errors.New("email and password are required")
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
