package filecount

import (
	"os"
	"path/filepath"
	"time"

	"github.com/influxdata/telegraf"
	"github.com/influxdata/telegraf/plugins/inputs"
)

const sampleConfig = `
  ## TODO: add comments
  directory = "/var/cache/apt/archives"
  name = "*.deb"
  recursive = false
  regular_only = true
  size = 0
  mtime = 0
`

type FileCount struct {
	Directory   string
	Name        string
	Recursive   bool
	RegularOnly bool
	Size        int64
	MTime       int64 `toml:"mtime"`
	fileFilters []fileFilterFunc
}

type findFunc func(os.FileInfo)
type fileFilterFunc func(os.FileInfo) (bool, error)

func (_ *FileCount) Description() string {
	return "Count files in one or more directories"
}

func (_ *FileCount) SampleConfig() string { return sampleConfig }

func rejectNilFilters(filters []fileFilterFunc) []fileFilterFunc {
	filtered := make([]fileFilterFunc, 0, len(filters))
	for _, f := range filters {
		if f != nil {
			filtered = append(filtered, f)
		}
	}
	return filtered
}

func readdir(directory string) ([]os.FileInfo, error) {
	f, err := os.Open(directory)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	files, err := f.Readdir(-1)
	if err != nil {
		return nil, err
	}
	return files, nil
}

func (fc *FileCount) nameFilter() fileFilterFunc {
	if fc.Name == "*" {
		return nil
	}

	return func(f os.FileInfo) (bool, error) {
		match, err := filepath.Match(fc.Name, f.Name())
		if err != nil {
			return false, err
		}
		return match, nil
	}
}

func (fc *FileCount) regularOnlyFilter() fileFilterFunc {
	if !fc.RegularOnly {
		return nil
	}

	return func(f os.FileInfo) (bool, error) {
		return f.Mode().IsRegular(), nil
	}
}

func (fc *FileCount) sizeFilter() fileFilterFunc {
	if fc.Size == 0 {
		return nil
	}

	return func(f os.FileInfo) (bool, error) {
		if !f.Mode().IsRegular() {
			return false, nil
		}
		if fc.Size < 0 {
			return f.Size() < -fc.Size, nil
		}
		return f.Size() >= fc.Size, nil
	}
}

func (fc *FileCount) mtimeFilter() fileFilterFunc {
	if fc.MTime == 0 {
		return nil
	}

	return func(f os.FileInfo) (bool, error) {
		age := time.Duration(absInt(fc.MTime)) * time.Second
		mtime := time.Now().Add(-age)
		if fc.MTime < 0 {
			return f.ModTime().After(mtime), nil
		}
		return f.ModTime().Before(mtime), nil
	}
}

func absInt(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func find(directory string, recursive bool, ff findFunc) error {
	files, err := readdir(directory)
	if err != nil {
		return err
	}

	for _, file := range files {
		path := filepath.Join(directory, file.Name())

		if recursive && file.IsDir() {
			err = find(path, recursive, ff)
			if err != nil {
				return err
			}
		}

		ff(file)
	}
	return nil
}

func (fc *FileCount) initFileFilters() {
	filters := []fileFilterFunc{
		fc.nameFilter(),
		fc.regularOnlyFilter(),
		fc.sizeFilter(),
		fc.mtimeFilter(),
	}
	fc.fileFilters = rejectNilFilters(filters)
}

func (fc *FileCount) filter(file os.FileInfo) (bool, error) {
	if fc.fileFilters == nil {
		fc.initFileFilters()
	}

	for _, fileFilter := range fc.fileFilters {
		match, err := fileFilter(file)
		if err != nil {
			return false, err
		}
		if !match {
			return false, nil
		}
	}

	return true, nil
}

func (fc *FileCount) Gather(acc telegraf.Accumulator) error {
	numFiles := int64(0)
	ff := func(f os.FileInfo) {
		match, err := fc.filter(f)
		if err != nil {
			acc.AddError(err)
			return
		}
		if !match {
			return
		}
		numFiles++
	}
	err := find(fc.Directory, fc.Recursive, ff)
	if err != nil {
		acc.AddError(err)
	}

	acc.AddFields("filecount",
		map[string]interface{}{
			"count": numFiles,
		},
		map[string]string{
			"directory": fc.Directory,
		})

	return nil
}

func NewFileCount() *FileCount {
	return &FileCount{
		Directory:   "",
		Name:        "*",
		Recursive:   true,
		RegularOnly: true,
		Size:        0,
		MTime:       0,
		fileFilters: nil,
	}
}

func init() {
	inputs.Add("filecount", func() telegraf.Input {
		return NewFileCount()
	})
}
