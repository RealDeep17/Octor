package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestList_sortItems_FoldersFirst(t *testing.T) {
	l := NewList()

	items := []ListItem{
		{Name: "file1.txt", Type: ListTypeFile, Size: 100},
		{Name: "aFolder", Type: ListTypeDirectory, Size: 500},
		{Name: "file2.txt", Type: ListTypeFile, Size: 200},
		{Name: "bFolder", Type: ListTypeDirectory, Size: 300},
	}

	l.sortItems(items, ListSortTypeName)

	// Verify folders come first
	assert.Equal(t, ListTypeDirectory, items[0].Type)
	assert.Equal(t, ListTypeDirectory, items[1].Type)
	assert.Equal(t, ListTypeFile, items[2].Type)
	assert.Equal(t, ListTypeFile, items[3].Type)

	// Verify alphabetical order within each type
	assert.Equal(t, "aFolder", items[0].Name)
	assert.Equal(t, "bFolder", items[1].Name)
	assert.Equal(t, "file1.txt", items[2].Name)
	assert.Equal(t, "file2.txt", items[3].Name)
}

func TestList_sortItems_AlphabeticalCaseInsensitive(t *testing.T) {
	l := NewList()

	items := []ListItem{
		{Name: "Zebra.txt", Type: ListTypeFile, Size: 100},
		{Name: "apple.txt", Type: ListTypeFile, Size: 200},
		{Name: "Banana.txt", Type: ListTypeFile, Size: 150},
	}

	l.sortItems(items, ListSortTypeName)

	assert.Equal(t, "apple.txt", items[0].Name)
	assert.Equal(t, "Banana.txt", items[1].Name)
	assert.Equal(t, "Zebra.txt", items[2].Name)
}

func TestList_sortItems_BySize(t *testing.T) {
	l := NewList()

	items := []ListItem{
		{Name: "small.txt", Type: ListTypeFile, Size: 100},
		{Name: "large.txt", Type: ListTypeFile, Size: 500},
		{Name: "medium.txt", Type: ListTypeFile, Size: 300},
		{Name: "folder", Type: ListTypeDirectory, Size: 1000},
	}

	l.sortItems(items, ListSortTypeSize)

	// Folder should still come first
	assert.Equal(t, ListTypeDirectory, items[0].Type)
	assert.Equal(t, "folder", items[0].Name)

	// Files sorted by size (largest first)
	assert.Equal(t, "large.txt", items[1].Name)
	assert.Equal(t, int64(500), items[1].Size)
	assert.Equal(t, "medium.txt", items[2].Name)
	assert.Equal(t, int64(300), items[2].Size)
	assert.Equal(t, "small.txt", items[3].Name)
	assert.Equal(t, int64(100), items[3].Size)
}

func TestList_sortItems_None(t *testing.T) {
	l := NewList()

	items := []ListItem{
		{Name: "file2.txt", Type: ListTypeFile, Size: 200},
		{Name: "aFolder", Type: ListTypeDirectory, Size: 500},
		{Name: "file1.txt", Type: ListTypeFile, Size: 100},
		{Name: "bFolder", Type: ListTypeDirectory, Size: 300},
	}

	l.sortItems(items, ListSortTypeNone)

	// Original order should be preserved — no sorting at all
	assert.Equal(t, "file2.txt", items[0].Name)
	assert.Equal(t, "aFolder", items[1].Name)
	assert.Equal(t, "file1.txt", items[2].Name)
	assert.Equal(t, "bFolder", items[3].Name)
}

func TestList_sortItems_SizeTieBreaker(t *testing.T) {
	l := NewList()

	items := []ListItem{
		{Name: "zebra.txt", Type: ListTypeFile, Size: 100},
		{Name: "apple.txt", Type: ListTypeFile, Size: 100},
	}

	l.sortItems(items, ListSortTypeSize)

	// When sizes are equal, should fall back to alphabetical
	assert.Equal(t, "apple.txt", items[0].Name)
	assert.Equal(t, "zebra.txt", items[1].Name)
}

func TestList_BuildTree_UnsortedFiles(t *testing.T) {
	l := NewList()

	r := &Resource{
		ID:   "dummy",
		Name: "dummy",
		Files: []*File{
			{Path: []string{"dummy", "01. Season 1 + Special", "file1.mkv"}, Size: 100},
			{Path: []string{"dummy", "02. OVA", "file2.mkv"}, Size: 200},
			{Path: []string{"dummy", "01. Season 1 + Special", "file3.mkv"}, Size: 300},
		},
	}

	args := &ListGetArgs{
		Output: ListOutputTypeTree,
		Path:   []string{"dummy"},
		Sort:   ListSortTypeName,
	}

	resp, err := l.Get(r, args)
	assert.NoError(t, err)

	// We expect two directories: "01. Season 1 + Special" and "02. OVA"
	// "01. Season 1 + Special" should have Size: 400 (100 + 300)
	// "02. OVA" should have Size: 200
	assert.Equal(t, 2, len(resp.Items))

	// Under SortTypeName, "01. Season 1 + Special" comes first, then "02. OVA"
	assert.Equal(t, "01. Season 1 + Special", resp.Items[0].Name)
	assert.Equal(t, int64(400), resp.Items[0].Size)
	assert.Equal(t, ListTypeDirectory, resp.Items[0].Type)

	assert.Equal(t, "02. OVA", resp.Items[1].Name)
	assert.Equal(t, int64(200), resp.Items[1].Size)
	assert.Equal(t, ListTypeDirectory, resp.Items[1].Type)
}

