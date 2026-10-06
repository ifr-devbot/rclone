package operations_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/accounting"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/fs/operations"
	"github.com/rclone/rclone/fs/walk"
	"github.com/rclone/rclone/fstest"
	"github.com/rclone/rclone/lib/random"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Check flag satisfies the interface
var _ pflag.Value = (*operations.DeduplicateMode)(nil)

func skipIfCantDedupe(t *testing.T, f fs.Fs) {
	if !f.Features().DuplicateFiles {
		t.Skip("Can't test deduplicate - no duplicate files possible")
	}
	if f.Features().PutUnchecked == nil {
		t.Skip("Can't test deduplicate - no PutUnchecked")
	}
	if f.Features().MergeDirs == nil {
		t.Skip("Can't test deduplicate - no MergeDirs")
	}
}

func skipIfNoHash(t *testing.T, f fs.Fs) {
	if f.Hashes().GetOne() == hash.None {
		t.Skip("Can't run this test without a hash")
	}
}

func skipIfNoModTime(t *testing.T, f fs.Fs) {
	if f.Precision() >= fs.ModTimeNotSupported {
		t.Skip("Can't run this test without modtimes")
	}
}

func TestDeduplicateInteractive(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfCantDedupe(t, r.Fremote)
	skipIfNoHash(t, r.Fremote)

	file1 := r.WriteUncheckedObject(context.Background(), "one", "This is one", t1)
	file2 := r.WriteUncheckedObject(context.Background(), "one", "This is one", t1)
	file3 := r.WriteUncheckedObject(context.Background(), "one", "This is one", t1)
	r.CheckWithDuplicates(t, file1, file2, file3)

	err := operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateInteractive, false, false)
	require.NoError(t, err)

	r.CheckRemoteItems(t, file1)
}

func TestDeduplicateSkip(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfCantDedupe(t, r.Fremote)
	haveHash := r.Fremote.Hashes().GetOne() != hash.None

	file1 := r.WriteUncheckedObject(context.Background(), "one", "This is one", t1)
	files := []fstest.Item{file1}
	if haveHash {
		file2 := r.WriteUncheckedObject(context.Background(), "one", "This is one", t1)
		files = append(files, file2)
	}
	file3 := r.WriteUncheckedObject(context.Background(), "one", "This is another one", t1)
	files = append(files, file3)
	r.CheckWithDuplicates(t, files...)

	err := operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateSkip, false, false)
	require.NoError(t, err)

	r.CheckWithDuplicates(t, file1, file3)
}

func TestDeduplicateSizeOnly(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfCantDedupe(t, r.Fremote)
	ctx := context.Background()
	ci := fs.GetConfig(ctx)

	file1 := r.WriteUncheckedObject(context.Background(), "one", "This is one", t1)
	file2 := r.WriteUncheckedObject(context.Background(), "one", "THIS IS ONE", t1)
	file3 := r.WriteUncheckedObject(context.Background(), "one", "This is another one", t1)
	r.CheckWithDuplicates(t, file1, file2, file3)

	ci.SizeOnly = true
	defer func() {
		ci.SizeOnly = false
	}()

	err := operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateSkip, false, false)
	require.NoError(t, err)

	r.CheckWithDuplicates(t, file1, file3)
}

func TestDeduplicateFirst(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfCantDedupe(t, r.Fremote)

	file1 := r.WriteUncheckedObject(context.Background(), "one", "This is one", t1)
	file2 := r.WriteUncheckedObject(context.Background(), "one", "This is one A", t1)
	file3 := r.WriteUncheckedObject(context.Background(), "one", "This is one BB", t1)
	r.CheckWithDuplicates(t, file1, file2, file3)

	err := operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateFirst, false, false)
	require.NoError(t, err)

	// list until we get one object
	var objects, size int64
	for try := 1; try <= *fstest.ListRetries; try++ {
		objects, size, _, err = operations.Count(context.Background(), r.Fremote)
		require.NoError(t, err)
		if objects == 1 {
			break
		}
		time.Sleep(time.Second)
	}
	assert.Equal(t, int64(1), objects)
	if size != file1.Size && size != file2.Size && size != file3.Size {
		t.Errorf("Size not one of the object sizes %d", size)
	}
}

func TestDeduplicateNewest(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfCantDedupe(t, r.Fremote)
	skipIfNoModTime(t, r.Fremote)

	file1 := r.WriteUncheckedObject(context.Background(), "one", "This is one", t1)
	file2 := r.WriteUncheckedObject(context.Background(), "one", "This is one too", t2)
	file3 := r.WriteUncheckedObject(context.Background(), "one", "This is another one", t3)
	r.CheckWithDuplicates(t, file1, file2, file3)

	err := operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateNewest, false, false)
	require.NoError(t, err)

	r.CheckRemoteItems(t, file3)
}

func TestDeduplicateNewestByHash(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfNoHash(t, r.Fremote)
	skipIfNoModTime(t, r.Fremote)
	contents := random.String(100)

	file1 := r.WriteObject(context.Background(), "one", contents, t1)
	file2 := r.WriteObject(context.Background(), "also/one", contents, t2)
	file3 := r.WriteObject(context.Background(), "another", contents, t3)
	file4 := r.WriteObject(context.Background(), "not-one", "stuff", t3)
	r.CheckRemoteItems(t, file1, file2, file3, file4)

	err := operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateNewest, true, false)
	require.NoError(t, err)

	r.CheckRemoteItems(t, file3, file4)
}

func TestDeduplicateNewestByHashLinks(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfNoHash(t, r.Fremote)
	skipIfNoModTime(t, r.Fremote)
	ctx := context.Background()
	contents := random.String(100)

	file1 := r.WriteObject(ctx, "one", contents, t1)
	file2 := r.WriteObject(ctx, "dir/sub/two", contents, t2)
	file3 := r.WriteObject(ctx, "dir/three", contents, t1)
	file4 := r.WriteObject(ctx, "other/four", contents, t2)
	file5 := r.WriteObject(ctx, "other/five", contents, t1)
	file6 := r.WriteObject(ctx, "dir/keep", contents, t3)
	file7 := r.WriteObject(ctx, "not-one", "stuff", t3)
	r.CheckRemoteItems(t, file1, file2, file3, file4, file5, file6, file7)

	err := operations.Deduplicate(ctx, r.Fremote, operations.DeduplicateNewest, true, true)
	require.NoError(t, err)

	want := []fstest.Item{
		fstest.NewItem("one"+fs.LinkSuffix, "dir/keep", t1),
		fstest.NewItem("dir/sub/two"+fs.LinkSuffix, "../keep", t2),
		fstest.NewItem("dir/three"+fs.LinkSuffix, "keep", t1),
		fstest.NewItem("other/four"+fs.LinkSuffix, "../dir/keep", t2),
		fstest.NewItem("other/five"+fs.LinkSuffix, "../dir/keep", t1),
		file6,
		file7,
	}
	r.CheckRemoteItems(t, want...)

	// The two links in other have the same hash but must be left alone
	err = operations.Deduplicate(ctx, r.Fremote, operations.DeduplicateNewest, true, true)
	require.NoError(t, err)
	r.CheckRemoteItems(t, want...)
}

func TestDeduplicateByHashLinksExisting(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfNoHash(t, r.Fremote)
	skipIfNoModTime(t, r.Fremote)
	ctx := context.Background()
	contents := random.String(100)

	file1 := r.WriteObject(ctx, "one", contents, t1)
	file2 := r.WriteObject(ctx, "two", contents, t3)
	file3 := r.WriteObject(ctx, "three", contents, t2)
	file4 := r.WriteObject(ctx, "one"+fs.LinkSuffix, "not a link to two", t1)
	r.CheckRemoteItems(t, file1, file2, file3, file4)

	accounting.Stats(ctx).ResetCounters()
	defer accounting.Stats(ctx).ResetCounters()
	err := operations.Deduplicate(ctx, r.Fremote, operations.DeduplicateNewest, true, true)
	require.NoError(t, err)
	assert.Equal(t, int64(1), accounting.Stats(ctx).GetErrors())

	r.CheckRemoteItems(t, file1, file2, fstest.NewItem("three"+fs.LinkSuffix, "two", t2), file4)
}

func TestDeduplicateByHashLinksDryRun(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfNoHash(t, r.Fremote)
	skipIfNoModTime(t, r.Fremote)
	contents := random.String(100)

	file1 := r.WriteObject(context.Background(), "one", contents, t1)
	file2 := r.WriteObject(context.Background(), "dir/two", contents, t2)
	r.CheckRemoteItems(t, file1, file2)

	ctx, ci := fs.AddConfig(context.Background())
	ci.DryRun = true
	err := operations.Deduplicate(ctx, r.Fremote, operations.DeduplicateNewest, true, true)
	require.NoError(t, err)

	r.CheckRemoteItems(t, file1, file2)
}

func TestDeduplicateLinksInvalid(t *testing.T) {
	r := fstest.NewRun(t)

	err := operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateNewest, false, true)
	assert.ErrorContains(t, err, "--by-hash")

	err = operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateRename, true, true)
	assert.ErrorContains(t, err, "rename")
}

func TestDeduplicateOldest(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfCantDedupe(t, r.Fremote)

	file1 := r.WriteUncheckedObject(context.Background(), "one", "This is one", t1)
	file2 := r.WriteUncheckedObject(context.Background(), "one", "This is one too", t2)
	file3 := r.WriteUncheckedObject(context.Background(), "one", "This is another one", t3)
	r.CheckWithDuplicates(t, file1, file2, file3)

	err := operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateOldest, false, false)
	require.NoError(t, err)

	r.CheckRemoteItems(t, file1)
}

func TestDeduplicateLargest(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfCantDedupe(t, r.Fremote)

	file1 := r.WriteUncheckedObject(context.Background(), "one", "This is one", t1)
	file2 := r.WriteUncheckedObject(context.Background(), "one", "This is one too", t2)
	file3 := r.WriteUncheckedObject(context.Background(), "one", "This is another one", t3)
	r.CheckWithDuplicates(t, file1, file2, file3)

	err := operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateLargest, false, false)
	require.NoError(t, err)

	r.CheckRemoteItems(t, file3)
}

func TestDeduplicateSmallest(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfCantDedupe(t, r.Fremote)

	file1 := r.WriteUncheckedObject(context.Background(), "one", "This is one", t1)
	file2 := r.WriteUncheckedObject(context.Background(), "one", "This is one too", t2)
	file3 := r.WriteUncheckedObject(context.Background(), "one", "This is another one", t3)
	r.CheckWithDuplicates(t, file1, file2, file3)

	err := operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateSmallest, false, false)
	require.NoError(t, err)

	r.CheckRemoteItems(t, file1)
}

func TestDeduplicateRename(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfCantDedupe(t, r.Fremote)

	file1 := r.WriteUncheckedObject(context.Background(), "one.txt", "This is one", t1)
	file2 := r.WriteUncheckedObject(context.Background(), "one.txt", "This is one too", t2)
	file3 := r.WriteUncheckedObject(context.Background(), "one.txt", "This is another one", t3)
	file4 := r.WriteUncheckedObject(context.Background(), "one-1.txt", "This is not a duplicate", t1)
	r.CheckWithDuplicates(t, file1, file2, file3, file4)

	err := operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateRename, false, false)
	require.NoError(t, err)

	require.NoError(t, walk.ListR(context.Background(), r.Fremote, "", true, -1, walk.ListObjects, func(entries fs.DirEntries) error {
		entries.ForObject(func(o fs.Object) {
			remote := o.Remote()
			if remote != "one-1.txt" &&
				remote != "one-2.txt" &&
				remote != "one-3.txt" &&
				remote != "one-4.txt" {
				t.Errorf("Bad file name after rename %q", remote)
			}
			size := o.Size()
			if size != file1.Size &&
				size != file2.Size &&
				size != file3.Size &&
				size != file4.Size {
				t.Errorf("Size not one of the object sizes %d", size)
			}
			if remote == "one-1.txt" && size != file4.Size {
				t.Errorf("Existing non-duplicate file modified %q", remote)
			}
		})
		return nil
	}))
}

// Check rename finds a free name when many numbered names already exist
func TestDeduplicateRenameManyExisting(t *testing.T) {
	r := fstest.NewRun(t)
	skipIfCantDedupe(t, r.Fremote)

	// Fill in one-1.txt to one-105.txt so the search has to go past 100
	const existing = 105
	var items []fstest.Item
	for i := 1; i <= existing; i++ {
		items = append(items, r.WriteObject(context.Background(), fmt.Sprintf("one-%d.txt", i), "This is not a duplicate", t1))
	}
	file1 := r.WriteUncheckedObject(context.Background(), "one.txt", "This is one", t1)
	file2 := r.WriteUncheckedObject(context.Background(), "one.txt", "This is one too", t2)
	items = append(items, file1, file2)
	r.CheckWithDuplicates(t, items...)

	err := operations.Deduplicate(context.Background(), r.Fremote, operations.DeduplicateRename, false, false)
	require.NoError(t, err)

	// The duplicates are renamed in listing order which isn't
	// defined, so accept either assignment of the two new names
	sizes := map[string]int64{}
	require.NoError(t, walk.ListR(context.Background(), r.Fremote, "", true, -1, walk.ListObjects, func(entries fs.DirEntries) error {
		entries.ForObject(func(o fs.Object) {
			sizes[o.Remote()] = o.Size()
		})
		return nil
	}))
	assert.Equal(t, existing+2, len(sizes))
	for i := 1; i <= existing; i++ {
		assert.Equal(t, items[i-1].Size, sizes[fmt.Sprintf("one-%d.txt", i)])
	}
	size1 := sizes[fmt.Sprintf("one-%d.txt", existing+1)]
	size2 := sizes[fmt.Sprintf("one-%d.txt", existing+2)]
	assert.ElementsMatch(t, []int64{file1.Size, file2.Size}, []int64{size1, size2})
}

// This should really be a unit test, but the test framework there
// doesn't have enough tools to make it easy
func TestMergeDirs(t *testing.T) {
	r := fstest.NewRun(t)

	mergeDirs := r.Fremote.Features().MergeDirs
	if mergeDirs == nil {
		t.Skip("Can't merge directories")
	}

	file1 := r.WriteObject(context.Background(), "dupe1/one.txt", "This is one", t1)
	file2 := r.WriteObject(context.Background(), "dupe2/two.txt", "This is one too", t2)
	file3 := r.WriteObject(context.Background(), "dupe3/three.txt", "This is another one", t3)

	objs, dirs, err := walk.GetAll(context.Background(), r.Fremote, "", true, 1)
	require.NoError(t, err)
	assert.Equal(t, 3, len(dirs))
	assert.Equal(t, 0, len(objs))

	err = mergeDirs(context.Background(), dirs)
	require.NoError(t, err)

	file2.Path = "dupe1/two.txt"
	file3.Path = "dupe1/three.txt"
	r.CheckRemoteItems(t, file1, file2, file3)

	objs, dirs, err = walk.GetAll(context.Background(), r.Fremote, "", true, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, len(dirs))
	assert.Equal(t, 0, len(objs))
	assert.Equal(t, "dupe1", dirs[0].Remote())
}
