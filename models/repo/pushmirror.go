// Copyright 2021 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package repo

import (
	"errors"
	"time"

	"code.gitea.io/gitea/models/db"
	"code.gitea.io/gitea/modules/log"
	"code.gitea.io/gitea/modules/timeutil"
)

// ErrPushMirrorNotExist mirror does not exist error
var ErrPushMirrorNotExist = errors.New("PushMirror does not exist")

// PushMirror represents mirror information of a repository.
type PushMirror struct {
	ID         int64       `xorm:"pk autoincr"`
	RepoID     int64       `xorm:"INDEX"`
	Repo       *Repository `xorm:"-"`
	RemoteName string

	SyncOnCommit   bool `xorm:"NOT NULL DEFAULT true"`
	Interval       time.Duration
	CreatedUnix    timeutil.TimeStamp `xorm:"created"`
	LastUpdateUnix timeutil.TimeStamp `xorm:"INDEX last_update"`
	LastError      string             `xorm:"text"`
}

func init() {
	db.RegisterModel(new(PushMirror))
}

// GetRepository returns the path of the repository.
func (m *PushMirror) GetRepository() *Repository {
	if m.Repo != nil {
		return m.Repo
	}
	var err error
	m.Repo, err = GetRepositoryByIDCtx(db.DefaultContext, m.RepoID)
	if err != nil {
		log.Error("getRepositoryByID[%d]: %v", m.ID, err)
	}
	return m.Repo
}

// GetRemoteName returns the name of the remote.
func (m *PushMirror) GetRemoteName() string {
	return m.RemoteName
}

// InsertPushMirror inserts a push-mirror to database
func InsertPushMirror(m *PushMirror) error {
	_, err := db.GetEngine(db.DefaultContext).Insert(m)
	return err
}

// UpdatePushMirror updates the push-mirror
func UpdatePushMirror(m *PushMirror) error {
	_, err := db.GetEngine(db.DefaultContext).ID(m.ID).AllCols().Update(m)
	return err
}

// DeletePushMirrorByID deletes a push-mirrors by ID
func DeletePushMirrorByID(ID int64) error {
	_, err := db.GetEngine(db.DefaultContext).ID(ID).Delete(&PushMirror{})
	return err
}

// DeletePushMirrorsByRepoID deletes all push-mirrors by repoID
func DeletePushMirrorsByRepoID(repoID int64) error {
	_, err := db.GetEngine(db.DefaultContext).Delete(&PushMirror{RepoID: repoID})
	return err
}

// GetPushMirrorByID returns push-mirror information.
func GetPushMirrorByID(ID int64) (*PushMirror, error) {
	m := &PushMirror{}
	has, err := db.GetEngine(db.DefaultContext).ID(ID).Get(m)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, ErrPushMirrorNotExist
	}
	return m, nil
}

// GetPushMirrorsByRepoID returns push-mirror information of a repository.
func GetPushMirrorsByRepoID(repoID int64) ([]*PushMirror, error) {
	mirrors := make([]*PushMirror, 0, 10)
	return mirrors, db.GetEngine(db.DefaultContext).Where("repo_id=?", repoID).Find(&mirrors)
}

// PushMirrorsIterate iterates all push-mirror repositories.
func PushMirrorsIterate(limit int, f func(idx int, bean interface{}) error) error {
	sess := db.GetEngine(db.DefaultContext).
		Where("last_update + (`interval` / ?) <= ?", time.Second, time.Now().Unix()).
		And("`interval` != 0").
		OrderBy("last_update ASC")
	if limit > 0 {
		sess = sess.Limit(limit)
	}
	return sess.Iterate(new(PushMirror), f)
}
