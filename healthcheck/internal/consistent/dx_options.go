package consistent

type Option func(o *option)

type option struct {
	weights      []int16
	maxWeight    int16
	searchLimit  int
	nsArrLen     int
	initialState NodeState
	newPrng      func() Prng
}

func WithNSArrayLen(l int) Option {
	return func(o *option) {
		o.nsArrLen = l
	}
}

func Weighted(weights []int16) Option {
	return func(o *option) {
		o.weights = weights
	}
}

func WithMaxWeight(maxWeight int16) Option {
	return func(o *option) {
		o.maxWeight = maxWeight
	}
}

func WithSearchLimit(searchLimit int) Option {
	return func(o *option) {
		o.searchLimit = searchLimit
	}
}

func WithPrng(new func() Prng) Option {
	return func(o *option) {
		o.newPrng = new
	}
}

func WithNewInitialState(state NodeState) Option {
	return func(o *option) {
		o.initialState = state
	}
}
