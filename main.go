// main
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dsoprea/go-exif"
)

var (
	errNoDateTimeOriginal = errors.New("Could not find DateTimeOriginal EXIF tag")
)

func extractDateTimeOriginal(path string) (dateTime time.Time, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errNoDateTimeOriginal
		}
	}()

	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".jpg" && ext != ".jpeg" {
		err = errNoDateTimeOriginal
		return
	}

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

func PathExists(path string) bool {
	_, err := os.Lstat(path)
	if err != nil {
		// if os.IsNotExist(err) {
		// 	return false
		// }
		return false
	}
	return true
}

type Source struct {
	File        os.FileInfo
	Dir         string
	Filename    string
	CreatedDate time.Time
}

func main() {
	doRelated := flag.Bool("related", false, "Include files with same filename yet different extension")
	useModTime := flag.Bool("modtime", false, "Use mod time if no EXIF tag found")
	doCopy := flag.Bool("cp", false, "Copy files (takes precedence over move)")
	doMove := flag.Bool("mv", false, "Move files")
	doSymlink := flag.Bool("symlink", false, "Symlink file to target folder")
	doHardlink := flag.Bool("hardlink", false, "Hard link file to target folder")
	doOverwrite := flag.Bool("overwrite", false, "Overwrite destination file if exists")
	useSuffixes := flag.Bool("suffixes", false, "If target file would be overwritten then generate a unique suffix")
	sourceFolder := flag.String("source", "", "Source folder to scan for JPEGs")
	doRecurse := flag.Bool("recurse", false, "Recurse into subdirectories of source folder")
	targetFolder := flag.String("target", ".", "Destination folder to copy/move files to")
	flag.Parse()

	if *doCopy && *doMove {
		*doMove = false
	}
	if *doSymlink && *doHardlink {
		*doHardlink = false
	}

	if *sourceFolder == "" {
		flag.Usage()
		os.Exit(-1)
		return
	}

	*sourceFolder = filepath.Clean(*sourceFolder)

	dirFiles := make(map[string][]string)

	sources := make([]*Source, 0, 10)
	err := filepath.Walk(*sourceFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("prevent panic by handling failure accessing a path %q: %v\n", path, err)
			return err
		}
		if info.IsDir() {
			if *doRecurse {
				return nil
			} else {
				return filepath.SkipDir
			}
		}

		// Split path into dir and filename:
		dir, filename := filepath.Split(path)
		if strings.HasPrefix(dir, *sourceFolder) {
			dir = dir[len(*sourceFolder):]
		}

		// Match filename:
		isJpg, _ := filepath.Match("*.[jJ][pP][gG]", filename)
		isJpeg, _ := filepath.Match("*.[jJ][pP][eE][gG]", filename)
		isPng, _ := filepath.Match("*.[pP][nN][gG]", filename)
		if isJpg || isJpeg || isPng {
			// Track this file as a source:
			dateTime, err := extractDateTimeOriginal(path)
			if err != nil {
				if *useModTime && err == errNoDateTimeOriginal {
					// Use file modification date if no EXIF tag found:
					dateTime = info.ModTime()
				} else {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", path, err)
					return nil
				}
			}

			sources = append(sources, &Source{
				File:        info,
				Dir:         dir,
				Filename:    filename,
				CreatedDate: dateTime,
			})
		} else if *doRelated {
			// Append filename to directory map:
			var fs []string
			var ok bool
			if fs, ok = dirFiles[dir]; !ok {
				fs = make([]string, 0, 10)
			}
			fs = append(fs, filename)
			dirFiles[dir] = fs
		}

		return nil
	})

	for _, source := range sources {
		dateTime := source.CreatedDate

		names := make([]string, 0, 2)
		names = append(names, source.Filename)

		// Find related files with same base name but different extension:
		if *doRelated {
			for _, fn := range dirFiles[source.Dir] {
				if strings.HasPrefix(fn, NoExt(source.Filename)) {
					names = append(names, fn)
				}
			}
		}

		// Generate timestamp base name:
		timestampFilename := dateTime.Format("20060102_150405")
		timestampFilename += fmt.Sprintf("_%03d", int64(time.Duration(dateTime.Nanosecond())/time.Millisecond))

		// Rename all related files to use timestamp:
	nextName:
		for _, name := range names {
			// srcPath is relative path from *sourceFolder but not including *sourceFolder prefix
			srcPath := filepath.Join(source.Dir, name)
			destExt := strings.ToLower(filepath.Ext(srcPath))

			// Generate destination path:
			destPath := filepath.Join(*targetFolder, source.Dir, timestampFilename+destExt)

			if !*doOverwrite {
				// Check if destination path exists:
				destPathExists := PathExists(destPath)
				if destPathExists {
					if *useSuffixes {
						// Generate a unique suffix and retry:
						for counter := 1; ; counter++ {
							destFilename := fmt.Sprintf("%s_%d%s", timestampFilename, counter, destExt)
							destPath = filepath.Join(*targetFolder, source.Dir, destFilename)
							if !PathExists(destPath) {
								break
							}
						}
					} else {
						fmt.Fprintf(os.Stderr, "\"%s\": Not overwriting existing file \"%s\"\n", srcPath, destPath)
						continue nextName
					}
				}
			}

			filePerm := os.FileMode(0644)
			if *doCopy || *doMove || *doSymlink || *doHardlink {
				stat := source.File

				// Take file permissions of original file:
				filePerm = stat.Mode() & os.ModePerm

				// Compute directory permissions by setting 'x' bit for each corresponding 'r' bit:
				// e.g. 'r--r--r--' => 'r-xr-xr-x'
				dirPerm := filePerm | ((filePerm & 0444) >> 2)

				// Make directory for target file to be contained in:
				err = os.MkdirAll(filepath.Dir(destPath), dirPerm)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", srcPath, err)
					continue nextName
				}

				// Remove target file if overwriting is enabled:
				if *doOverwrite {
					os.Remove(destPath)
				}
			}

			// Figure out what to do with the file:
			if *doCopy {
				fmt.Printf("cp \"%s\" \"%s\"\n", srcPath, destPath)

				// Open source file for reading:
				fin, err := os.Open(srcPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", srcPath, err)
					continue nextName
				}

				// Open target file for writing:
				fout, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, filePerm)
				if err != nil {
					fin.Close()
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", srcPath, err)
					continue nextName
				}

				// Copy file contents from source to target in 4096 byte chunks:
				buf := make([]byte, 4096)
				n := 4096
				for n > 0 {
					// Read from source:
					n, err = fin.Read(buf)
					if err == io.EOF {
						break
					}
					if err != nil {
						fin.Close()
						fout.Close()
						fmt.Fprintf(os.Stderr, "\"%s\": %v\n", srcPath, err)
						continue nextName
					}

					// Write to target:
					_, err = fout.Write(buf[0:n])
					if err != nil {
						fin.Close()
						fout.Close()
						fmt.Fprintf(os.Stderr, "\"%s\": %v\n", srcPath, err)
						continue nextName
					}
				}

				fin.Close()
				fout.Close()

				// Set mod time of target file to that of source file:
				err = os.Chtimes(destPath, time.Now(), dateTime)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", srcPath, err)
					continue nextName
				}
			} else if *doMove {
				fmt.Printf("mv \"%s\" \"%s\"\n", srcPath, destPath)
				err := os.Rename(srcPath, destPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", srcPath, err)
				}
			} else if *doSymlink {
				relName, err := filepath.Rel(*targetFolder, srcPath)
				fmt.Printf("symlink \"%s\" \"%s\"\n", srcPath, destPath)
				err = os.Symlink(relName, destPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", srcPath, err)
				}
			} else if *doHardlink {
				relName, err := filepath.Rel(*targetFolder, srcPath)
				fmt.Printf("hardlink \"%s\" \"%s\"\n", srcPath, destPath)
				err = os.Link(relName, destPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", srcPath, err)
				}
			} else {
				fmt.Printf("\"%s\" \"%s\"\n", srcPath, destPath)
			}
		}
	}
}
