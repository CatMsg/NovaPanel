package service

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
