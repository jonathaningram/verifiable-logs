package generalisedtransparency

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/continusec/verifiabledatastructures/verifiable"
	"github.com/govau/verifiable-logs/assets"
)

// CreateRESTHandler returns an http.Handler for our REST API
func (cts *Server) CreateRESTHandler() http.Handler {
	r := mux.NewRouter()

	// REST API
	cts.addCallToRouter(r, "/metadata", cts.ReadAPIKey, true, cts.handleMetadata)
	cts.addCallToRouter(r, "/add-objecthash", cts.WriteAPIKey, false, cts.handleAdd)
	cts.addCallToRouter(r, "/get-objecthash", cts.ReadAPIKey, true, cts.handleGetObjectHash)
	cts.addCallToRouter(r, "/get-sth", cts.ReadAPIKey, true, cts.handleSTH)
	cts.addCallToRouter(r, "/get-sth-consistency", cts.ReadAPIKey, true, cts.handleSTHConsistency)
	cts.addCallToRouter(r, "/get-proof-by-hash", cts.ReadAPIKey, true, cts.handleProofByHash)
	cts.addCallToRouter(r, "/get-entries", cts.ReadAPIKey, true, cts.handleGetEntries)
	cts.addCallToRouter(r, "/get-entry-and-proof", cts.ReadAPIKey, true, cts.handleGetEntryAndProof)

	// Static
	r.HandleFunc("/dataset/{logname}/", cts.staticHandler("text/html", "index.html"))
	r.HandleFunc("/verifiable.js", cts.staticHandler("application/javascript", "verifiable.js"))
	r.HandleFunc("/sha256.js", cts.staticHandler("application/javascript", "sha256.js"))
	r.HandleFunc("/", cts.staticHandler("text/html", "root.html"))

	// Convenience redirect
	r.HandleFunc("/dataset/{logname}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.RequestURI()+"/", http.StatusMovedPermanently)
	})

	return r
}

func (cts *Server) staticHandler(mime, name string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Println(r.URL.String())
		data, err := assets.Asset("assets/static/" + name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", mime)
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	}
}

func (cts *Server) wrapCall(apiKey string, ensureExists bool, f func(log *verifiable.Log, r *http.Request) (interface{}, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Println(r.URL.String())

		// Make sure table is a valid to prevent us from making an inadvertent call to the wrong path
		canonTable, err := cts.TableNameValidator.ValidateAndCanonicaliseTableName(mux.Vars(r)["logname"])
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		vlog := cts.Service.Account(cts.Account, apiKey).VerifiableLog(canonTable)
		if ensureExists {
			// This is to make sure we don't spuriously create way too many tables in postgresql for logs that don't exists
			_, err = cts.getSigningKey(r.Context(), vlog, false)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
		}
		obj, err := f(vlog, r)
		if err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(obj)
			return
		}

		// Some errors are status code errors
		s, ok := status.FromError(err)
		if ok {
			switch s.Code() {
			case codes.PermissionDenied:
				http.Error(w, err.Error(), http.StatusForbidden)
			case codes.InvalidArgument:
				http.Error(w, err.Error(), http.StatusBadRequest)
			case codes.NotFound:
				http.Error(w, err.Error(), http.StatusNotFound)
			default:
				log.Println(err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		switch err {
		case verifiable.ErrInvalidRequest, verifiable.ErrInvalidRange, verifiable.ErrInvalidTreeRange:
			http.Error(w, err.Error(), http.StatusBadRequest)
		case verifiable.ErrNotFound:
			http.Error(w, err.Error(), http.StatusNotFound)
		case verifiable.ErrNotAuthorized:
			http.Error(w, err.Error(), http.StatusForbidden)
		default:
			log.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

func (cts *Server) addCallToRouter(r *mux.Router, path, apiKey string, ensureExists bool, f func(log *verifiable.Log, r *http.Request) (interface{}, error)) {
	r.HandleFunc("/dataset/{logname}/ct/v1"+path, cts.wrapCall(apiKey, ensureExists, f))
}
