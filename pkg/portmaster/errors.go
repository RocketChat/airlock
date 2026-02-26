package portmaster

import "fmt"

type ReconcilerError struct {
	Reason string
	Err    error
}

func (e *ReconcilerError) Error() string {
	return fmt.Errorf("%s: %w", e.Reason, e.Err).Error()
}

func NewRuntimeError(err error) *ReconcilerError {
	return &ReconcilerError{
		Reason: "RuntimeError",
		Err:    err,
	}
}

func NewSecretKeyNotFoundError(err error) *ReconcilerError {
	return &ReconcilerError{
		Reason: "SecretKeyNotFound",
		Err:    err,
	}
}

func NewMongoDBConnectionError(err error) *ReconcilerError {
	return &ReconcilerError{
		Reason: "MongoDBConnectionError",
		Err:    err,
	}
}

func NewMongoDBAccessRequestReadyTimeoutError(err error) *ReconcilerError {
	return &ReconcilerError{
		Reason: "MongoDBAccessRequestReadyTimeout",
		Err:    err,
	}
}

func NewInvalidDestinationBucketError(err error) *ReconcilerError {
	return &ReconcilerError{
		Reason: "InvalidDestinationBucket",
		Err:    err,
	}
}

func NewDiskSizeEstimateError(err error) *ReconcilerError {
	return &ReconcilerError{
		Reason: "DiskSizeEstimateError",
		Err:    err,
	}
}
