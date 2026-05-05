package automation

import (
	"encoding/json"
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

var (
	ErrGoalNotFound = errors.New("goal not found")
	goalsBucket     = []byte("goals")
)

func (s *Store) GetGoal(scope Scope) (GoalTask, error) {
	if s == nil {
		return GoalTask{}, errors.New("store is nil")
	}
	scope = normalizeScope(scope)
	if scope.Kind == "" || scope.ID == "" {
		return GoalTask{}, errors.New("scope is empty")
	}
	var goal GoalTask
	db, err := s.dbOrOpen()
	if err != nil {
		return GoalTask{}, err
	}
	err = db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(goalsBucket)
		if b == nil {
			return ErrGoalNotFound
		}
		key := goalScopeKey(scope)
		data := b.Get(key)
		if data == nil {
			return ErrGoalNotFound
		}
		var err error
		goal, err = decodeGoal(data)
		return err
	})
	if err != nil {
		return GoalTask{}, err
	}
	return goal, nil
}

func (s *Store) ReplaceGoal(goal GoalTask) (GoalTask, error) {
	if s == nil {
		return GoalTask{}, errors.New("store is nil")
	}
	goal = NormalizeGoal(goal)
	now := s.nowLocal()
	if goal.ID == "" {
		return GoalTask{}, errors.New("goal id is empty")
	}
	if goal.CreatedAt.IsZero() {
		goal.CreatedAt = now
	}
	goal.UpdatedAt = now
	if err := ValidateGoal(goal); err != nil {
		return GoalTask{}, err
	}
	var created GoalTask
	db, err := s.dbOrOpen()
	if err != nil {
		return GoalTask{}, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(goalsBucket)
		if err != nil {
			return err
		}
		key := goalScopeKey(goal.Scope)
		existing := b.Get(key)
		if existing != nil {
			existingGoal, err := decodeGoal(existing)
			if err != nil {
				return err
			}
			if existingGoal.Status != GoalStatusComplete && existingGoal.Status != GoalStatusTimeout {
				return fmt.Errorf("scope already has an active or paused goal (id=%s)", existingGoal.ID)
			}
		}
		data, err := encodeGoal(goal)
		if err != nil {
			return err
		}
		if err := b.Put(key, data); err != nil {
			return err
		}
		created = goal
		return nil
	})
	if err != nil {
		return GoalTask{}, err
	}
	return created, nil
}

func (s *Store) PatchGoal(scope Scope, mutate func(goal *GoalTask) error) (GoalTask, error) {
	if s == nil {
		return GoalTask{}, errors.New("store is nil")
	}
	scope = normalizeScope(scope)
	if scope.Kind == "" || scope.ID == "" {
		return GoalTask{}, errors.New("scope is empty")
	}
	if mutate == nil {
		return GoalTask{}, errors.New("mutate callback is nil")
	}
	var updated GoalTask
	db, err := s.dbOrOpen()
	if err != nil {
		return GoalTask{}, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(goalsBucket)
		if b == nil {
			return ErrGoalNotFound
		}
		key := goalScopeKey(scope)
		data := b.Get(key)
		if data == nil {
			return ErrGoalNotFound
		}
		goal, err := decodeGoal(data)
		if err != nil {
			return err
		}
		if err := mutate(&goal); err != nil {
			return err
		}
		goal = NormalizeGoal(goal)
		goal.UpdatedAt = s.nowLocal()
		goal.Revision++
		if err := ValidateGoal(goal); err != nil {
			return err
		}
		data, err = encodeGoal(goal)
		if err != nil {
			return err
		}
		if err := b.Put(key, data); err != nil {
			return err
		}
		updated = goal
		return nil
	})
	if err != nil {
		return GoalTask{}, err
	}
	return updated, nil
}

func (s *Store) DeleteGoal(scope Scope) error {
	if s == nil {
		return errors.New("store is nil")
	}
	scope = normalizeScope(scope)
	if scope.Kind == "" || scope.ID == "" {
		return errors.New("scope is empty")
	}
	db, err := s.dbOrOpen()
	if err != nil {
		return err
	}
	return db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(goalsBucket)
		if b == nil {
			return ErrGoalNotFound
		}
		key := goalScopeKey(scope)
		if b.Get(key) == nil {
			return ErrGoalNotFound
		}
		return b.Delete(key)
	})
}

func goalScopeKey(scope Scope) []byte {
	scope = normalizeScope(scope)
	return []byte(fmt.Sprintf("%s:%s", scope.Kind, scope.ID))
}

func encodeGoal(goal GoalTask) ([]byte, error) {
	return json.Marshal(goal)
}

func decodeGoal(data []byte) (GoalTask, error) {
	var goal GoalTask
	if err := json.Unmarshal(data, &goal); err != nil {
		return GoalTask{}, err
	}
	return NormalizeGoal(goal), nil
}
