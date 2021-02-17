package s3pg

import (
	"database/sql"

	"github.com/pkg/errors"
	installationtypes "github.com/replicatedhq/kots/kotsadm/pkg/online/types"
	"github.com/replicatedhq/kots/pkg/persistence"
)

func (s S3PGStore) GetPendingInstallationStatus() (*installationtypes.InstallStatus, error) {
	db := persistence.MustGetPGSession()
	query := `SELECT install_state from app ORDER BY created_at DESC LIMIT 1`
	row := db.QueryRow(query)

	var installState sql.NullString
	if err := row.Scan(&installState); err != nil {
		if err == sql.ErrNoRows {
			return &installationtypes.InstallStatus{
				InstallStatus:  "not_installed",
				CurrentMessage: "",
			}, nil
		}
		return nil, errors.Wrap(err, "failed to scan")
	}

	_, message, err := s.GetTaskStatus("online-install")
	if err != nil {
		return nil, errors.Wrap(err, "failed to get task status")
	}

	status := &installationtypes.InstallStatus{
		InstallStatus:  installState.String,
		CurrentMessage: message,
	}

	return status, nil
}
