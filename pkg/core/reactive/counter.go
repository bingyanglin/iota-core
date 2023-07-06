package reactive

import "github.com/iotaledger/hive.go/lo"

// Counter is a Variable that derives its value from the number of times the monitored input values fulfill a certain
// condition.
type Counter[InputType comparable] interface {
	// Variable holds the counter value.
	Variable[int]

	// Monitor adds the given input value as an input to the counter and returns a function that can be used to
	// unsubscribe from the input value.
	Monitor(input Value[InputType]) (unsubscribe func())
}

// NewCounter creates a Counter that counts the number of times monitored input values fulfill a certain condition.
func NewCounter[InputType comparable](condition ...func(inputValue InputType) bool) Counter[InputType] {
	return &counter[InputType]{
		Variable: NewVariable[int](),
		condition: lo.First(condition, func(newInputValue InputType) bool {
			var zeroValue InputType
			return newInputValue != zeroValue
		}),
	}
}

// counter implements the Counter interface.
type counter[InputType comparable] struct {
	// variable is the ValueReceptor that holds the output value of the counter.
	Variable[int]

	// condition is the condition that is used to determine whether the input value fulfills the counted criteria.
	condition func(inputValue InputType) bool
}

// Monitor subscribes to updates of the given input and returns a function that can be used to unsubscribe again.
func (t *counter[InputType]) Monitor(input Value[InputType]) (unsubscribe func()) {
	return input.OnUpdate(func(oldInputValue, newInputValue InputType) {
		t.Compute(func(currentThreshold int) int {
			return lo.Cond(t.condition(newInputValue), currentThreshold+1, currentThreshold-1)
		})
	})
}
