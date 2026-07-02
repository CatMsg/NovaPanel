package service

import "os"

func combinePostCommitActions(actions ...func() error) func() error {
	filtered := make([]func() error, 0, len(actions))
	for _, action := range actions {
		if action != nil {
			filtered = append(filtered, action)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	return func() error {
		for _, action := range filtered {
			if err := action(); err != nil {
				return err
			}
		}
		return nil
	}
}

type coreReplaceSnapshot struct {
	removeTag string
	config    []byte
	beforeAdd func(string) error
}

func buildCoreReplaceAction(
	snapshots []coreReplaceSnapshot,
	remove func(string) error,
	add func([]byte) error,
) func() error {
	if len(snapshots) == 0 {
		return nil
	}

	return func() error {
		for _, snapshot := range snapshots {
			if snapshot.removeTag != "" {
				err := remove(snapshot.removeTag)
				if err != nil && err != os.ErrInvalid {
					return err
				}
			}
			if snapshot.beforeAdd != nil {
				if err := snapshot.beforeAdd(snapshot.removeTag); err != nil {
					return err
				}
			}
			if len(snapshot.config) > 0 {
				if err := add(snapshot.config); err != nil {
					return err
				}
			}
		}
		return nil
	}
}
