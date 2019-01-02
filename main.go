// main
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dsoprea/go-exif"
)

var errNoDateTimeOriginal = errors.New("Could not find DateTimeOriginal EXIF tag")

func extractDateTimeOriginal(path string) (dateTime time.Time, err error) {
	rawExif, err := exif.SearchFileAndExtractExif(path)
	if err != nil {
		return
	}

	im := exif.NewIfdMapping()

	err = exif.LoadStandardIfds(im)
	if err != nil {
		return
	}

	ti := exif.NewTagIndex()

	_, index, err := exif.Collect(im, ti, rawExif)
	if err != nil {
		return
	}

	// Read DateTimeOriginal plus SubSecTimeOriginal:
	exifIfd, err := index.RootIfd.ChildWithIfdPath("IFD/Exif")
	if err != nil {
		return
	}

	results, err := exifIfd.FindTagWithName("DateTimeOriginal")
	if len(results) == 0 {
		err = errNoDateTimeOriginal
		return
	}

	dateTimeOriginal, err := index.RootIfd.TagValue(results[0])
	if err != nil {
		return
	}

	results, err = exifIfd.FindTagWithName("SubSecTimeOriginal")
	var subSecTimeOriginal interface{}
	if len(results) == 1 {
		subSecTimeOriginal, err = index.RootIfd.TagValue(results[0])
		if err != nil {
			return
		}
	} else {
		subSecTimeOriginal = "000"
	}

	dateTimeFmt := dateTimeOriginal.(string) + "." + subSecTimeOriginal.(string)
	dateTime, err = time.Parse("2006:01:02 15:04:05.999", dateTimeFmt)

	return
}

func NoExt(path string) string {
	for i := len(path) - 1; i >= 0 && !os.IsPathSeparator(path[i]); i-- {
		if path[i] == '.' {
			return path[0:i]
		}
	}
	return ""
}

func main() {
	doRelated := flag.Bool("related", false, "Include files with same filename yet different extension")
	useModTime := flag.Bool("modtime", false, "Use mod time if no EXIF tag found")
	// doMove := flag.Bool("move", false, "Move files")
	// doCopy := flag.Bool("copy", false, "Copy files")
	// targetFolder := flag.String("dest", ".", "Destination folder to copy/move files to")
	flag.Parse()

	args := flag.Args()

	if len(args) <= 1 {
		flag.Usage()
		os.Exit(-1)
		return
	}

	dirs := make(map[string][]os.FileInfo)

	paths := args[1:]
	for _, p := range paths {
		names := make([]string, 0, 2)
		names = append(names, p)

		// Find related filenames with different extensions:
		if *doRelated {
			dirname := filepath.Dir(p)

			var dir []os.FileInfo
			var ok bool
			if dir, ok = dirs[dirname]; !ok {
				var err error
				dir, err = ioutil.ReadDir(dirname)
				if err == nil {
					dirs[dirname] = dir
				}
			}

			if dir != nil {
				for _, f := range dir {
					if f.Name() == p {
						continue
					}
					if strings.HasPrefix(f.Name(), NoExt(p)) {
						names = append(names, f.Name())
					}
				}
			}
		}

		dateTime, err := extractDateTimeOriginal(p)
		if err != nil {
			if *useModTime && err == errNoDateTimeOriginal {
				// Use file modification date if no EXIF tag found:
				stat, err := os.Stat(p)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", p, err)
					continue
				}
				dateTime = stat.ModTime()
			} else {
				fmt.Fprintf(os.Stderr, "\"%s\": %v\n", p, err)
				continue
			}
		}

		timestampFilename := dateTime.Format("20060102_150405")
		timestampFilename += fmt.Sprintf("_%03d", int64(time.Duration(dateTime.Nanosecond())/time.Millisecond))

		// Rename all related files to use timestamp:
		for _, name := range names {
			fmt.Printf("\"%s\"\t\"%s\"\n", name, timestampFilename+strings.ToLower(filepath.Ext(name)))
		}
	}
}
