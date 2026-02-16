package errors

import (
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

type AggregateError struct {
	errors []error
}

func New() *AggregateError {
	return &AggregateError{
		errors: []error{},
	}
}

func (e *AggregateError) HasErrors() bool {
	return len(e.errors) > 0
}

func (e *AggregateError) Append(errs ...error) *AggregateError {
	for _, err := range errs {
		if err != nil {
			e.errors = append(e.errors, err)
		}
	}

	return e
}

func (e *AggregateError) Aggregate() utilerrors.Aggregate {
	return utilerrors.NewAggregate(e.errors)
}

func (e *AggregateError) Error() string {
	return utilerrors.NewAggregate(e.errors).Error()
}

func (e *AggregateError) Is(target error) bool {
	return e.Aggregate().Is(target)
}

func (e *AggregateError) IfExists() error {
	if e.HasErrors() {
		return e.Aggregate()
	}

	return nil
}
