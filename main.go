// main
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/dsoprea/go-exif"
)

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
	dateTimeOriginal, err := index.RootIfd.TagValue(results[0])
	if err != nil {
		return
	}

	results, err = exifIfd.FindTagWithName("SubSecTimeOriginal")
	subSecTimeOriginal, err := index.RootIfd.TagValue(results[0])
	if err != nil {
		return
	}

	dateTimeFmt := dateTimeOriginal.(string) + "." + subSecTimeOriginal.(string)
	dateTime, err = time.Parse("2006:01:02 15:04:05.999", dateTimeFmt)

	return
}

func main() {
	dateTime, err := extractDateTimeOriginal("IMG_1847.JPG")
	if err != nil {
		log.Panic(err)
	}

	fmt.Println(dateTime)
}
