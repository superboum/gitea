// Copyright 2015 The Gogs Authors. All rights reserved.
// Copyright 2019 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

//go:build !gogit

package git

import (
	"errors"
	"io"

	"code.gitea.io/gitea/modules/log"
)

// IsTagExist returns true if given tag exists in the repository.
func (repo *Repository) IsTagExist(name string) bool {
	if repo == nil || name == "" {
		return false
	}

	return repo.IsReferenceExist(TagPrefix + name)
}

// GetTags returns all tags of the repository.
// returning at most limit tags, or all if limit is 0.
func (repo *Repository) GetTags(skip, limit int) (tags []string, err error) {
	tags, _, err = callShowRef(repo.Ctx, repo.Path, TagPrefix, "--tags", skip, limit)
	return
}

// GetTagType gets the type of the tag, either commit (simple) or tag (annotated)
func (repo *Repository) GetTagType(id SHA1) (string, error) {
	wr, rd, cancel := repo.CatFileBatchCheck(repo.Ctx)
	defer cancel()
	_, err := wr.Write([]byte(id.String() + "\n"))
	if err != nil {
		return "", err
	}
	_, typ, _, err := ReadBatchLine(rd)
	if IsErrNotExist(err) {
		return "", ErrNotExist{ID: id.String()}
	}
	return typ, nil
}

func (repo *Repository) getTag(tagID SHA1, name string) (*Tag, error) {
	t, ok := repo.tagCache.Get(tagID.String())
	if ok {
		log.Debug("Hit cache: %s", tagID)
		tagClone := *t.(*Tag)
		tagClone.Name = name // This is necessary because lightweight tags may have same id
		return &tagClone, nil
	}

	tp, err := repo.GetTagType(tagID)
	if err != nil {
		return nil, err
	}

	// Get the commit ID and tag ID (may be different for annotated tag) for the returned tag object
	commitIDStr, err := repo.GetTagCommitID(name)
	if err != nil {
		// every tag should have a commit ID so return all errors
		return nil, err
	}
	commitID, err := NewIDFromString(commitIDStr)
	if err != nil {
		return nil, err
	}

	// If type is "commit, the tag is a lightweight tag
	if ObjectType(tp) == ObjectCommit {
		commit, err := repo.GetCommit(commitIDStr)
		if err != nil {
			return nil, err
		}
		tag := &Tag{
			Name:    name,
			ID:      tagID,
			Object:  commitID,
			Type:    tp,
			Tagger:  commit.Committer,
			Message: commit.Message(),
		}

		repo.tagCache.Set(tagID.String(), tag)
		return tag, nil
	}

	// The tag is an annotated tag with a message.
	wr, rd, cancel := repo.CatFileBatch(repo.Ctx)
	defer cancel()

	if _, err := wr.Write([]byte(tagID.String() + "\n")); err != nil {
		return nil, err
	}
	_, typ, size, err := ReadBatchLine(rd)
	if err != nil {
		if errors.Is(err, io.EOF) || IsErrNotExist(err) {
			return nil, ErrNotExist{ID: tagID.String()}
		}
		return nil, err
	}
	if typ != "tag" {
		return nil, ErrNotExist{ID: tagID.String()}
	}

	// then we need to parse the tag
	// and load the commit
	data, err := io.ReadAll(io.LimitReader(rd, size))
	if err != nil {
		return nil, err
	}
	_, err = rd.Discard(1)
	if err != nil {
		return nil, err
	}

	tag, err := parseTagData(data)
	if err != nil {
		return nil, err
	}

	tag.Name = name
	tag.ID = tagID
	tag.Type = tp

	repo.tagCache.Set(tagID.String(), tag)
	return tag, nil
}
