// main
package main

import (
	"errors"
	"fmt"
	"os"
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

func main() {
	if len(os.Args) <= 1 {
		fmt.Println("Expected JPEG path argument(s)")
		os.Exit(-1)
		return
	}

	paths := os.Args[1:]
	for _, path := range paths {
		dateTime, err := extractDateTimeOriginal(path)
		if err != nil {
			if err == errNoDateTimeOriginal {
				// Use file modification date if no EXIF tag found:
				stat, err := os.Stat(path)
				if err != nil {
					fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
					continue
				}
				dateTime = stat.ModTime()
			} else {
				fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
				continue
			}
		}

		timestampFilename := dateTime.Format("20060102_150405")
		timestampFilename += fmt.Sprintf("_%03d.jpg", int64(time.Duration(dateTime.Nanosecond())/time.Millisecond))
		fmt.Printf("%s\t%s\n", path, timestampFilename)
	}
}
