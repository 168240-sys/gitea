// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitServRepoPath(t *testing.T) {
	owner, repo, ok := splitServRepoPath("org3/repo3.git")
	assert.True(t, ok)
	assert.Equal(t, "org3", owner)
	assert.Equal(t, "repo3", repo)

	owner, repo, ok = splitServRepoPath("/org3/backend/api.git")
	assert.True(t, ok)
	assert.Equal(t, "org3", owner)
	assert.Equal(t, "backend/api", repo)

	owner, repo, ok = splitServRepoPath("org3/backend/api.wiki.git")
	assert.True(t, ok)
	assert.Equal(t, "org3", owner)
	assert.Equal(t, "backend/api.wiki", repo)

	_, _, ok = splitServRepoPath("org3")
	assert.False(t, ok)
	_, _, ok = splitServRepoPath("org3//repo")
	assert.False(t, ok)
}
