package internal

import "fmt"

type TargetConfig struct {
	Username string
	Password string
}

func (c *TargetConfig) Validate() error {
	if c.Username == "" {
		return fmt.Errorf("username is required")
	} else if c.Password == "" {
		return fmt.Errorf("password is required")
	}
	return nil
}
