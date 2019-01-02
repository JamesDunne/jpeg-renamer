// main
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
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

func main() {
	doRelated := flag.Bool("related", false, "Include files with same filename yet different extension")
	useModTime := flag.Bool("modtime", false, "Use mod time if no EXIF tag found")
	doCopy := flag.Bool("cp", false, "Copy files (takes precedence over move)")
	doMove := flag.Bool("mv", false, "Move files")
	doSymlink := flag.Bool("symlink", false, "Symlink file to target folder")
	doHardlink := flag.Bool("hardlink", false, "Hard link file to target folder")
	doOverwrite := flag.Bool("overwrite", false, "Overwrite destination file if exists")
	useSuffixes := flag.Bool("suffixes", false, "If target file would be overwritten then generate a unique suffix")
	targetFolder := flag.String("target", ".", "Destination folder to copy/move files to")
	flag.Parse()

	if *doCopy && *doMove {
		*doMove = false
	}
	if *doSymlink && *doHardlink {
		*doHardlink = false
	}

	args := flag.Args()

	if len(args) == 0 {
		flag.Usage()
		os.Exit(-1)
		return
	}

	dirs := make(map[string][]os.FileInfo)

	paths := args[:]
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
	nextName:
		for _, name := range names {
			// Generate destination path:
			destPath := filepath.Join(*targetFolder, timestampFilename+strings.ToLower(filepath.Ext(name)))

			if !*doOverwrite {
				// Check if destination path exists:
				destPathExists := PathExists(destPath)
				if destPathExists {
					if *useSuffixes {
						// Generate a unique suffix and retry:
						for counter := 1; ; counter++ {
							destFilename := fmt.Sprintf("%s_%d%s", timestampFilename, counter, strings.ToLower(filepath.Ext(name)))
							destPath = filepath.Join(*targetFolder, destFilename)
							if !PathExists(destPath) {
								break
							}
						}
					} else {
						fmt.Fprintf(os.Stderr, "\"%s\": Not overwriting existing file \"%s\"\n", name, destPath)
						continue nextName
					}
				}
			}

			filePerm := os.FileMode(0644)
			if *doCopy || *doMove || *doSymlink || *doHardlink {
				stat, err := os.Stat(name)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", name, err)
					continue nextName
				}

				// Take file permissions of original file:
				filePerm = stat.Mode() & os.ModePerm

				// Compute directory permissions by setting 'x' bit for each corresponding 'r' bit:
				// e.g. 'r--r--r--' => 'r-xr-xr-x'
				dirPerm := filePerm | ((filePerm & 0444) >> 2)

				// Make directory for target file to be contained in:
				err = os.MkdirAll(filepath.Dir(destPath), dirPerm)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", name, err)
					continue nextName
				}

				// Remove target file if overwriting is enabled:
				if *doOverwrite {
					os.Remove(destPath)
				}
			}

			// Figure out what to do with the file:
			if *doCopy {
				fmt.Printf("cp \"%s\" \"%s\"\n", name, destPath)

				// Open source file for reading:
				fin, err := os.Open(name)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", name, err)
					continue nextName
				}

				// Open target file for writing:
				fout, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, filePerm)
				if err != nil {
					fin.Close()
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", name, err)
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
						fmt.Fprintf(os.Stderr, "\"%s\": %v\n", name, err)
						continue nextName
					}

					// Write to target:
					_, err = fout.Write(buf[0:n])
					if err != nil {
						fin.Close()
						fout.Close()
						fmt.Fprintf(os.Stderr, "\"%s\": %v\n", name, err)
						continue nextName
					}
				}

				fin.Close()
				fout.Close()

				// Set mod time of target file to that of source file:
				err = os.Chtimes(destPath, time.Now(), dateTime)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", name, err)
					continue nextName
				}
			} else if *doMove {
				fmt.Printf("mv \"%s\" \"%s\"\n", name, destPath)
				err := os.Rename(name, destPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", name, err)
				}
			} else if *doSymlink {
				relName, err := filepath.Rel(*targetFolder, name)
				fmt.Printf("symlink \"%s\" \"%s\"\n", name, destPath)
				err = os.Symlink(relName, destPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", name, err)
				}
			} else if *doHardlink {
				relName, err := filepath.Rel(*targetFolder, name)
				fmt.Printf("hardlink \"%s\" \"%s\"\n", name, destPath)
				err = os.Link(relName, destPath)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\"%s\": %v\n", name, err)
				}
			} else {
				fmt.Printf("\"%s\"\t\"%s\"\n", name, destPath)
			}
		}
	}
}
