package main

// Web Server

import (
	"flag"
	"fmt"
	. "game/core"
	"http"
	"image"
	"json"
	"log"
	"os"
	"runtime"
	"runtime/pprof"
	"template"
	"time"
	"websocket"
)

type ClientId int

var nextClientId = make(chan ClientId)

func init() {
	go func() {
		for i := 1; ; i++ {
			nextClientId <- ClientId(i)
		}
	}()
}

type subscription struct {
	id        ClientId
	encoder   *json.Encoder
	subscribe bool
}

type Event struct {
	Type string
	X, Y int
}

type ClientEvent struct {
	id    ClientId
	event Event
}

//type ServerEvent struct {
//	event    Event
//}

var subscriptionChan = make(chan subscription)
var clientEventChan = make(chan ClientEvent)
//var serverEventChan = make(chan ServerEvent)

func clientHandler(ws *websocket.Conn) {
	id := <-nextClientId
	decoder := json.NewDecoder(ws)
	encoder := json.NewEncoder(ws)

	defer func() {
		fmt.Printf("Un subscribing\n")
		subscriptionChan <- subscription{id, encoder, false}
		ws.Close()
	}()

	fmt.Printf("Subscribing\n")

	subscriptionChan <- subscription{id, encoder, true}

	// Convert incoming web socket messages to events
	var event Event

	for {
		if e := decoder.Decode(&event); e != nil {
			break
		}
		clientEventChan <- ClientEvent{id, event}
	}
}

func hub(pile *Pile, filename string) {
	connections := make(map[ClientId]*json.Encoder)
	isDirty := false
	//t := time.Tick(15e9)
	t := time.Tick(15e12)

	for {
		select {
		case <-t:
			fmt.Printf("Time\n")
			if isDirty {
				fmt.Printf("Dirty: %s\n", *pile)
				for ii :=0; ii<100; ii++ {
					pile.Store(filename)
				}
				isDirty = false
			}

		case subscription := <-subscriptionChan:
			fmt.Printf("Subscribed: %v\n", subscription)
			connections[subscription.id] = subscription.encoder, subscription.subscribe

		//case serverEvent := <-serverEventChan:
		//	pile.Store(filename)

		case clientEvent := <-clientEventChan:
			//fmt.Printf("Event: %v\n", clientEvent)

			switch clientEvent.event.Type {
			case "REPAINT":
				connections[clientEvent.id].Encode(ClearCommand{"CLEAR"})

				pile.VisitFragments(func(pile *Pile, id CardId, fragments []*image.Rectangle) {
					//fmt.Printf("serve sending %v, %d\n", id, len(fragments))

					card := pile.Cards[id]

					for _, fragment := range fragments {
						cmd := card.Command(fragment)

						if e := connections[clientEvent.id].Encode(cmd); e != nil {
							log.Printf("server 2: %v", e)
						}
					}
				})

			case "CLICK":
				if fragmentMap, err := pile.Remove(clientEvent.event.X, clientEvent.event.Y); err == nil {
					isDirty = true

					for cardId, fragments := range fragmentMap {
						card := pile.Cards[cardId]

						for _, fragment := range fragments {
							cmd := card.Command(fragment)

							for _, connection := range connections {
								if e := connection.Encode(cmd); e != nil {
									log.Printf("2: %v", e)
								}
							}
						}
					}
				}

			default:
				fmt.Printf("Unknown: %v\n", clientEvent)
			}
		}
	}
}


func makeHomePage(height, width int, url string) http.HandlerFunc {
	type Tokens struct {
		Height int
		Width  int
		Url    string
	}

	tokens := Tokens{height, width, url}

	return func(rw http.ResponseWriter, r *http.Request) {
		fmt.Println("MakeHomePage")
		t := template.New(nil)
		t.SetDelims("{{{", "}}}")

		err := t.ParseFile("src/game/web/page.html")
		if err != nil {
			fmt.Printf("Error %v\n", err)
			http.Error(rw, err.String(), http.StatusInternalServerError)
			return
		}

		err = t.Execute(rw, tokens)
		if err != nil {
			http.Error(rw, err.String(), http.StatusInternalServerError)
		}
	}
}


func makeDumpPage() http.HandlerFunc {

	return func(rw http.ResponseWriter, r *http.Request) {
		f, err := os.Create(*memProfile)
	if err != nil {
		log.Fatalf("can't create %s: %s", *memProfile, err)
	}

	if err := pprof.WriteHeapProfile(f); err != nil {
		log.Fatalf("can't write %s: %s", *memProfile, err)
	}
	f.Close()
		fmt.Println("MakeDumpPage")
	}
}


var cpuProfile = flag.String("cpuprofile", "", "write cpu profile to file")
var memProfile = flag.String("memprofile", "", "write memory profile to file")
var memProfileRate = flag.Int("memprofilerate", runtime.MemProfileRate, "read godoc runtime MemProfileRate")

func main() {

	flag.Parse()

	if *memProfileRate != runtime.MemProfileRate {
		runtime.MemProfileRate = *memProfileRate
	}

	if *memProfile != "" {
		f, err := os.Create(*memProfile)
		if err != nil {
			log.Fatalf("can't create %s: %s", *memProfile, err)
		}
		defer func() {
			if err := pprof.WriteHeapProfile(f); err != nil {
				log.Fatalf("can't write %s: %s", *memProfile, err)
			}
			f.Close()
		}()
	}

	if *cpuProfile != "" {
		f, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatalf("can't create %s: %s", *cpuProfile, err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	url := flag.Arg(0)
	filename := flag.Arg(1)

	pile, err := Load(filename)
	if err != nil {
		fmt.Printf("Load error %v\n", err)
	}

	go hub(pile, filename)

	http.HandleFunc("/", makeHomePage(pile.Height, pile.Width, url))
	http.HandleFunc("/dump", makeDumpPage())
	http.Handle("/files/", http.StripPrefix("/files", http.FileServer(http.Dir("/tmp"))))
	http.Handle("/ws", websocket.Handler(clientHandler))

	println("Listening ...\n")
	err = http.ListenAndServe(url, nil)
	if err != nil {
		panic("Shit " + err.String())
	}
}
