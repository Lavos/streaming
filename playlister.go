package streaming

type Playlister interface {
	Status() chan Status
	Done()
}
