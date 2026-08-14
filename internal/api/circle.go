package api

import (
	"fmt"

	"github.com/cerclbackup/cerclbackup/internal/circle"
)

func circleManager(password string) (*circle.Manager, error) {
	if password == "" {
		return nil, fmt.Errorf("password is required")
	}
	ks, err := OpenKeystore(password)
	if err != nil {
		return nil, err
	}
	return circle.NewManager(ks, password), nil
}

// CircleAdd creates a new circle with the given name and RS scheme (e.g. "3/2").
func CircleAdd(password, name, scheme string) (*circle.Circle, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	mgr, err := circleManager(password)
	if err != nil {
		return nil, err
	}
	return mgr.Add(name, scheme)
}

// CircleList returns all configured circles.
func CircleList(password string) ([]*circle.Circle, error) {
	mgr, err := circleManager(password)
	if err != nil {
		return nil, err
	}
	return mgr.List()
}

// CircleRemove deletes the circle with the given name.
func CircleRemove(password, name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	mgr, err := circleManager(password)
	if err != nil {
		return err
	}
	return mgr.Remove(name)
}
