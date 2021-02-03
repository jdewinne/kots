package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/replicatedhq/kots/kotsadm/pkg/logger"
	"github.com/replicatedhq/kots/kotsadm/pkg/preflight"
	preflighttypes "github.com/replicatedhq/kots/kotsadm/pkg/preflight/types"
	"github.com/replicatedhq/kots/kotsadm/pkg/store"
	"github.com/replicatedhq/kots/pkg/kotsutil"
)

type GetPreflightResultResponse struct {
	PreflightResult preflighttypes.PreflightResult `json:"preflightResult"`
}

type GetPreflightCommandRequest struct {
	Origin string `json:"origin"`
}

type GetPreflightCommandResponse struct {
	Command []string `json:"command"`
}

func (h *Handler) GetPreflightResult(w http.ResponseWriter, r *http.Request) {
	appSlug := mux.Vars(r)["appSlug"]
	sequence, err := strconv.ParseInt(mux.Vars(r)["sequence"], 10, 64)
	if err != nil {
		logger.Error(err)
		w.WriteHeader(400)
		return
	}

	foundApp, err := store.GetStore().GetAppFromSlug(appSlug)
	if err != nil {
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	result, err := store.GetStore().GetPreflightResults(foundApp.ID, sequence)
	if err != nil {
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	response := GetPreflightResultResponse{
		PreflightResult: *result,
	}
	JSON(w, 200, response)
}

func (h *Handler) GetLatestPreflightResultsForSequenceZero(w http.ResponseWriter, r *http.Request) {
	result, err := store.GetStore().GetLatestPreflightResultsForSequenceZero()
	if err != nil {
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	skipPreflights, _ := strconv.ParseBool(r.URL.Query().Get("skipPreflights"))

	if skipPreflights {
		license, err := store.GetStore().GetLatestLicenseForApp(result.AppID)
		if err != nil {
			logger.Error(err)
			w.WriteHeader(500)
			return
		}

		req, err := sendPreflightsReportToReplicatedApp(license.Spec.LicenseID, result.AppSlug, skipPreflights, result.InstallState, false)
		if err != nil {
			fmt.Printf("%+v\n", err)
			logger.Error(err)
			w.WriteHeader(500)
			return
		}

		fmt.Println("____--", req)
	}

	response := GetPreflightResultResponse{
		PreflightResult: *result,
	}
	JSON(w, 200, response)
}

func (h *Handler) IgnorePreflightRBACErrors(w http.ResponseWriter, r *http.Request) {
	appSlug := mux.Vars(r)["appSlug"]
	sequence, err := strconv.Atoi(mux.Vars(r)["sequence"])
	if err != nil {
		logger.Error(err)
		w.WriteHeader(400)
		return
	}

	foundApp, err := store.GetStore().GetAppFromSlug(appSlug)
	if err != nil {
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	if err := store.GetStore().SetIgnorePreflightPermissionErrors(foundApp.ID, int64(sequence)); err != nil {
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	archiveDir, err := ioutil.TempDir("", "kotsadm")
	if err != nil {
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	removeArchiveDir := true
	defer func() {
		if removeArchiveDir {
			os.RemoveAll(archiveDir)
		}
	}()

	err = store.GetStore().GetAppVersionArchive(foundApp.ID, int64(sequence), archiveDir)
	if err != nil {
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	removeArchiveDir = false
	go func() {
		defer os.RemoveAll(archiveDir)
		if err := preflight.Run(foundApp.ID, foundApp.Slug, int64(sequence), foundApp.IsAirgap, archiveDir); err != nil {
			logger.Error(err)
			return
		}
	}()

	JSON(w, 200, struct{}{})
}

func (h *Handler) StartPreflightChecks(w http.ResponseWriter, r *http.Request) {
	appSlug := mux.Vars(r)["appSlug"]
	sequence, err := strconv.Atoi(mux.Vars(r)["sequence"])
	if err != nil {
		logger.Error(err)
		w.WriteHeader(400)
		return
	}

	foundApp, err := store.GetStore().GetAppFromSlug(appSlug)
	if err != nil {
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	if err := store.GetStore().ResetPreflightResults(foundApp.ID, int64(sequence)); err != nil {
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	archiveDir, err := ioutil.TempDir("", "kotsadm")
	if err != nil {
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	removeArchiveDir := true
	defer func() {
		if removeArchiveDir {
			os.RemoveAll(archiveDir)
		}
	}()

	err = store.GetStore().GetAppVersionArchive(foundApp.ID, int64(sequence), archiveDir)
	if err != nil {
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	removeArchiveDir = false
	go func() {
		defer os.RemoveAll(archiveDir)
		if err := preflight.Run(foundApp.ID, foundApp.Slug, int64(sequence), foundApp.IsAirgap, archiveDir); err != nil {
			logger.Error(err)
			return
		}
	}()

	JSON(w, 200, struct{}{})
}

func (h *Handler) GetPreflightCommand(w http.ResponseWriter, r *http.Request) {
	appSlug := mux.Vars(r)["appSlug"]
	sequence, err := strconv.ParseInt(mux.Vars(r)["sequence"], 10, 64)
	if err != nil {
		logger.Error(errors.Wrap(err, "failed to parse sequence"))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	inCluster := r.URL.Query().Get("inCluster") == "true"

	getPreflightCommandRequest := GetPreflightCommandRequest{}
	if err := json.NewDecoder(r.Body).Decode(&getPreflightCommandRequest); err != nil {
		logger.Error(errors.Wrap(err, "failed to decode request body"))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	foundApp, err := store.GetStore().GetAppFromSlug(appSlug)
	if err != nil {
		logger.Error(errors.Wrap(err, "failed to get app"))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	archivePath, err := ioutil.TempDir("", "kotsadm")
	if err != nil {
		logger.Error(errors.Wrap(err, "failed to create temp dir"))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(archivePath)

	err = store.GetStore().GetAppVersionArchive(foundApp.ID, sequence, archivePath)
	if err != nil {
		logger.Error(errors.Wrap(err, "failed to get app archive"))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	kotsKinds, err := kotsutil.LoadKotsKindsFromPath(archivePath)
	if err != nil {
		logger.Error(errors.Wrap(err, "failed to load kots kinds"))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = preflight.CreateRenderedSpec(foundApp.ID, sequence, getPreflightCommandRequest.Origin, inCluster, kotsKinds.Preflight)
	if err != nil {
		logger.Error(errors.Wrap(err, "failed to render preflight spec"))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	response := GetPreflightCommandResponse{
		Command: preflight.GetPreflightCommand(foundApp.Slug),
	}

	JSON(w, http.StatusOK, response)
}

// PostPreflightStatus route is UNAUTHENTICATED
// This request comes from the `kubectl preflight` command.
func (h *Handler) PostPreflightStatus(w http.ResponseWriter, r *http.Request) {
	appSlug := mux.Vars(r)["appSlug"]
	sequence, err := strconv.ParseInt(mux.Vars(r)["sequence"], 10, 64)
	if err != nil {
		err = errors.Wrap(err, "failed to parse sequence")
		logger.Error(err)
		w.WriteHeader(400)
		return
	}

	foundApp, err := store.GetStore().GetAppFromSlug(appSlug)
	if err != nil {
		err = errors.Wrap(err, "failed to get app from slug")
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		err = errors.Wrap(err, "failed to read request body")
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	if err := store.GetStore().SetPreflightResults(foundApp.ID, sequence, b); err != nil {
		err = errors.Wrap(err, "failed to set preflight results")
		logger.Error(err)
		w.WriteHeader(500)
		return
	}

	w.WriteHeader(204)
}

func sendPreflightsReportToReplicatedApp(licenseID string, appSlug string, skipPreflights bool, installStatus string, hasWarnOrErr bool) (bool, error) {
	urlValues := url.Values{}

	skipPreflightsToStr := fmt.Sprintf("%t", skipPreflights)
	hasWarnOrErrToStr := fmt.Sprintf("%t", hasWarnOrErr)

	urlValues.Set("skipPreflights", skipPreflightsToStr)
	urlValues.Set("installStatus", installStatus)
	urlValues.Set("hasWarnOrErr", hasWarnOrErrToStr)

	url := fmt.Sprintf("http://replicated-app.default.svc.cluster.local:3000/preflights/reporting/%s?%s", appSlug, urlValues.Encode())

	var buf bytes.Buffer
	postReq, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		return false, errors.Wrap(err, "failed to call newrequest")
	}
	postReq.Header.Add("Authorization", licenseID)
	postReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(postReq)
	if err != nil {
		return false, errors.Wrap(err, "failed to check for updates")
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false, errors.Wrap(err, "failed to read")
	}

	fmt.Printf("%s\n", b)

	return false, nil
}
