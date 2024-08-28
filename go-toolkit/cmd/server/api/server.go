package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/pprof"
	"path"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/opds-community/libopds2-go/opds2"
	"github.com/pkg/errors"
	"github.com/readium/go-toolkit/pkg/asset"
	"github.com/readium/go-toolkit/pkg/manifest"
	"github.com/readium/go-toolkit/pkg/pub"
	"github.com/readium/go-toolkit/pkg/streamer"
	"github.com/rs/cors"
	"github.com/sashabaranov/go-openai"
	"github.com/sirupsen/logrus"
	"github.com/urfave/negroni"
)

type PublicationServer struct {
	config    ServerConfig
	feed      *opds2.Feed
	apiSecret string
}

func NewPublicationServer(config ServerConfig, apiSecret string) *PublicationServer {
	return &PublicationServer{
		config:    config,
		feed:      new(opds2.Feed),
		apiSecret: apiSecret,
	}
}

func (s *PublicationServer) Init() http.Handler {
	n := negroni.New(negroni.NewStatic(http.Dir(s.config.StaticPath)))
	n.UseHandler(s.bookHandler(false))
	return n
}

func (s *PublicationServer) bookHandler(test bool) http.Handler {
	r := mux.NewRouter()

	r.HandleFunc("/debug/pprof/", pprof.Index)
	r.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	r.HandleFunc("/debug/pprof/profile", pprof.Profile)
	r.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	r.HandleFunc("/debug/pprof/trace", pprof.Trace)

	r.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))
	r.Handle("/debug/pprof/block", pprof.Handler("block"))
	r.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	r.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	r.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	r.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))

	// New API Route
	r.HandleFunc("/api/streamAIResponse", s.handleOpenAIRequest).Methods("POST")

	r.HandleFunc("/list.json", s.demoList)
	r.HandleFunc("/{filename}/manifest.json", s.getManifest)
	// r.HandleFunc("/{filename}/search", s.search)
	// r.HandleFunc("/{filename}/media-overlay", s.mediaOverlay)
	r.HandleFunc("/{filename}/{asset:.*}", s.getAsset)

	c := cors.New(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173"},
		AllowCredentials: true,
	})

	handler := c.Handler(r)
	log.Fatal(http.ListenAndServe(":5080", handler))

	return r
}

func makeRelative(link manifest.Link) manifest.Link {
	link.Href = strings.TrimPrefix(link.Href, "/")
	for i, alt := range link.Alternates {
		link.Alternates[i].Href = strings.TrimPrefix(alt.Href, "/")
	}
	return link
}

type demoListItem struct {
	Filename string `json:"filename"`
	Path     string `json:"path"`
}

func (s *PublicationServer) demoList(w http.ResponseWriter, req *http.Request) {
	fi, err := ioutil.ReadDir(s.config.PublicationPath)
	if err != nil {
		logrus.Error(err)
		w.WriteHeader(500)
		return
	}
	files := make([]demoListItem, len(fi))
	for i, f := range fi {
		files[i] = demoListItem{
			Filename: f.Name(),
			Path:     base64.RawURLEncoding.EncodeToString([]byte(f.Name())),
		}
	}
	json.NewEncoder(w).Encode(files)
}

func (s *PublicationServer) getPublication(filename string, r *http.Request) (*pub.Publication, error) {
	fpath, err := base64.RawURLEncoding.DecodeString(filename)
	if err != nil {
		return nil, err
	}

	cp := filepath.Clean(string(fpath))
	pub, err := streamer.New(streamer.Config{}).Open(asset.File(filepath.Join(s.config.PublicationPath, cp)), "")
	if err != nil {
		return nil, errors.Wrap(err, "failed opening "+cp)
	}

	// TODO standardize this!
	for i, link := range pub.Manifest.Resources {
		pub.Manifest.Resources[i] = makeRelative(link)
	}
	for i, link := range pub.Manifest.ReadingOrder {
		pub.Manifest.ReadingOrder[i] = makeRelative(link)
	}
	for i, link := range pub.Manifest.TableOfContents {
		pub.Manifest.TableOfContents[i] = makeRelative(link)
	}
	for i, link := range pub.Manifest.Links {
		pub.Manifest.Links[i] = makeRelative(link)
	}
	var makeCollectionRelative func(mp manifest.PublicationCollectionMap)
	makeCollectionRelative = func(mp manifest.PublicationCollectionMap) {
		for i := range mp {
			for j := range mp[i] {
				for k := range mp[i][j].Links {
					mp[i][j].Links[k] = makeRelative(mp[i][j].Links[k])
				}
				makeCollectionRelative(mp[i][j].Subcollections)
			}
		}
	}
	makeCollectionRelative(pub.Manifest.Subcollections)

	return pub, nil
}

func (s *PublicationServer) getManifest(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	filename := vars["filename"]

	publication, err := s.getPublication(filename, req)
	if err != nil {
		logrus.Error(err)
		w.WriteHeader(500)
		return
	}
	defer publication.Close()

	j, err := json.Marshal(publication.Manifest)
	if err != nil {
		logrus.Error(err)
		w.WriteHeader(500)
		return
	}

	mime := "application/webpub+json; charset=utf-8"
	for _, profile := range publication.Manifest.Metadata.ConformsTo {
		if profile == "https://readium.org/webpub-manifest/profiles/divina" {
			mime = "application/divina+json; charset=utf-8"
		} else if profile == "https://readium.org/webpub-manifest/profiles/audiobook" {
			mime = "application/audiobook+json; charset=utf-8"
		} else {
			continue
		}
		break
	}
	w.Header().Set("Content-Type", mime)

	w.Header().Set("Access-Control-Allow-Origin", "*") // TODO replace with CORS middleware

	var identJSON bytes.Buffer
	json.Indent(&identJSON, j, "", "  ")
	if err != nil {
		logrus.Error(err)
		w.WriteHeader(500)
		return
	}
	hashJSONRaw := sha256.Sum256(identJSON.Bytes())
	hashJSON := base64.RawURLEncoding.EncodeToString(hashJSONRaw[:])

	if match := req.Header.Get("If-None-Match"); match != "" {
		if strings.Contains(match, hashJSON) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.Header().Set("Etag", hashJSON)

	/*links := publication.GetPreFetchResources()
	if len(links) > 0 {
		prefetch := ""
		for _, l := range links {
			prefetch = prefetch + "<" + l.Href + ">;" + "rel=prefetch,"
		}
		w.Header().Set("Link", prefetch)
	}*/

	identJSON.WriteTo(w)
}

func (s *PublicationServer) getAsset(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	filename := vars["filename"]

	publication, err := s.getPublication(filename, r)
	if err != nil {
		logrus.Error(err)
		w.WriteHeader(500)
		return
	}
	defer publication.Close()

	href := path.Clean(vars["asset"])
	link := publication.Find(href)
	if link == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	res := publication.Get(*link)
	defer res.Close()
	/*if res.File() != "" {
		// Shortcut to serve the file in an optimal way
		http.ServeFile(w, r, res.File())
		return
	}*/

	w.Header().Set("Access-Control-Allow-Origin", "*") // TODO replace with CORS middleware
	w.Header().Set("Content-Type", link.MediaType().String())
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")

	_, rerr := res.Stream(w, 0, 0) // TODO byte range support
	if rerr != nil {
		w.WriteHeader(rerr.HTTPStatus())
		w.Write([]byte(rerr.Error()))
		return
	}
}

func (s *PublicationServer) handleOpenAIRequest(w http.ResponseWriter, r *http.Request) {

	// Set headers for chunked transfer encoding
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Transfer-Encoding", "chunked")

	// vars := mux.Vars(r)
	// message := vars["message"]
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Parse the JSON from the body
	var requestData struct {
		Message string `json:"message"`
	}
	err = json.Unmarshal(body, &requestData)
	if err != nil {
		http.Error(w, "Error parsing JSON", http.StatusBadRequest)
		return
	}
	message := requestData.Message

	fmt.Print("\nCLIENT CONTENT___________________________")
	fmt.Print(message)

	client := openai.NewClient(s.apiSecret)
	ctx := context.Background()

	req := openai.ChatCompletionRequest{
		Model: openai.GPT4oMini,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "You are a storyteller. Your goal is to educate with stories. The following story is a dialogue between a girl (Lily) and an inventor (Turing). The setting is fantasy, so please refrain from mentioning modern technology. The goal of the story is to educate the user about turing machines and the fundamentals of computation. The first few lines of dialogue are pre-written, but later lines by Lily will be written by a the user. You will occupy the role of Turing. Continue to teach Lily about computers, ask her questions, and respond to any questions she has. Feel free to progress the plot as you see fit. Lily will need to learn about computers to hijack the security systems (door locks, guards, etc.) and escape. Note that Lily is trapped in her cell and will need to learn how to break out. She and Turing will communicate only by note for the majority of the story. You may also include story events, but don't take actions for Lily. Always let the user respond before continuing the scene. Please write one response from Turing. Your response should be no more than a few sentences and should start with 'Turing: '.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: message,
			},
		},
		Stream: true,
	}

	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		fmt.Printf("ChatCompletionStream error: %v\n", err)
		return
	}
	defer stream.Close()

	// Use a buffer to write chunks of data to the response
	buffer := make([]byte, 0, 1024) // Buffer to accumulate chunks
	fmt.Print("\nAPI RESPONSE____________________________")
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			fmt.Println("\nSTREAM FINISHED_____________________________")
			return
		}
		if err != nil {
			http.Error(w, fmt.Sprintf("Stream error: %v", err), http.StatusInternalServerError)
			return
		}

		// Write received content to the response
		if response.Choices[0].Delta.Content != "" {
			fmt.Printf(response.Choices[0].Delta.Content)
			buffer = append(buffer, []byte(response.Choices[0].Delta.Content)...)
			if len(buffer) > 0 {
				_, err := w.Write(buffer)
				if err != nil {
					fmt.Printf("Write error: %v\n", err)
					return
				}
				w.(http.Flusher).Flush() // Ensure data is sent to the client
				buffer = buffer[:0]      // Reset buffer
			}
		}
	}
}
