package savepoint

func (opts OptionsOf[T]) hasRestoreTransition() bool {
	return opts.PrepareRestoreTransition != nil || opts.PrepareRestoreTransitionWithManifest != nil
}
