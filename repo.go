package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type distMetafile struct {
	filename string
	size     int64
	sha256   string
}

type repo struct {
	conf         repoConfig
	DownloadRoot string
}

func newRepo(r repoConfig) *repo {
	repo := &repo{
		conf: r,
	}

	if repo.conf.Proto == "" {
		repo.conf.Proto = "http"
	}
	repo.conf.URL = strings.TrimRight(repo.conf.URL, "/")

	return repo
}

func (r *repo) download(conf *config) error {
	r.DownloadRoot = conf.Skel

	// Download dist Release and release.gpg files if verify GPG
	log.Println("Downloading distribution index")
	if err := r.downloadDistRelease(conf.Skel); err != nil {
		return err
	}

	// Read Release and map Packages to hashes
	log.Println("Processing distribution index")
	distFiles, err := r.processDistRelease()
	if err != nil {
		return err
	}

	// Download each $component/$arch Packages file (prefer compressed if available) and check hash
	log.Println("Downloading component files")
	if err := r.downloadComponentFiles(distFiles); err != nil {
		return err
	}

	// Read Packages and map package filenames to hashes
	log.Println("Processing component files")
	archiveFiles, err := r.processPackages()
	if err != nil {
		return err
	}

	totalSize := int64(0)
	for _, m := range archiveFiles {
		totalSize += m.size
	}

	log.Printf("Total Packages to Download: %s\n", formatFileSize(totalSize))

	// Download package files and check hashes
	log.Println("Downloading packages")
	start := time.Now()
	if err := r.downloadPackages(archiveFiles, conf.DownloadWorkers); err != nil {
		return nil
	}

	log.Printf("Download took %s\n", time.Now().Sub(start).String())

	// Move packages and copy metadata to destination directory
	return nil
}

func (r *repo) downloadDistRelease(root string) error {
	dest := fmt.Sprintf("%s/dists/%s/InRelease", r.conf.URL, r.conf.Dist)
	if _, err := r.downloadRemoteFile(dest); err != nil {
		return err
	}

	dest = fmt.Sprintf("%s/dists/%s/Release", r.conf.URL, r.conf.Dist)
	if _, err := r.downloadRemoteFile(dest); err != nil {
		return err
	}

	dest = fmt.Sprintf("%s/dists/%s/Release.gpg", r.conf.URL, r.conf.Dist)
	if _, err := r.downloadRemoteFile(dest); err != nil {
		return err
	}

	if !r.conf.DisableGPG {
		return r.verifyReleaseSig(root)
	}
	return nil
}

func (r *repo) verifyReleaseSig(root string) error {
	// TODO: Verify GPG signature
	return nil
}

func (r *repo) processDistRelease() (map[string]distMetafile, error) {
	path := filepath.Join(r.DownloadRoot, r.conf.URL, "dists", r.conf.Dist, "Release")
	release, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer release.Close()

	files := make(map[string]distMetafile)

	reader := bufio.NewScanner(release)

	mode := "kv"
	for reader.Scan() {
		line := reader.Bytes()
		if mode == "hash" && line[0] != ' ' {
			mode = "kv"
		}

		if mode == "kv" {
			keyValue := bytes.SplitN(line, []byte{':'}, 2)

			if bytes.Equal(keyValue[0], []byte("Architectures")) {
				arches := bytes.Split(keyValue[1], []byte{' '})

				for _, arch := range r.conf.Archs {
					if !inSliceBytes(arches, []byte(arch)) {
						return nil, fmt.Errorf("Repository doesn't support arch %s", arch)
					}
				}
			} else if bytes.Equal(keyValue[0], []byte("Components")) {
				components := bytes.Split(keyValue[1], []byte{' '})

				for _, component := range r.conf.Components {
					if !inSliceBytes(components, []byte(component)) {
						return nil, fmt.Errorf("Repository doesn't have component %s", component)
					}
				}
			} else if bytes.Equal(keyValue[0], []byte("SHA256")) {
				logDebug("SHA256 Hashes:")
				mode = "hash"
			}
		} else if mode == "hash" {
			hashParts := bytes.Fields(bytes.TrimSpace(line))
			logDebugf(" %s -> %s -> %s", hashParts[0], hashParts[1], hashParts[2])

			size, _ := strconv.ParseInt(string(hashParts[1]), 10, 64)
			files[string(hashParts[2])] = distMetafile{
				size:   size,
				sha256: string(hashParts[0]),
			}
		}
	}

	return files, nil
}

func (r *repo) downloadComponentFiles(metafiles map[string]distMetafile) error {
	filenames := make(map[string]distMetafile, (len(r.conf.Components)+len(r.conf.Archs))*2)

	for _, component := range r.conf.Components {
		for _, arch := range r.conf.Archs {
			packageBase := fmt.Sprintf("%s/binary-%s/Packages", component, arch)
			releaseFile := fmt.Sprintf("%s/binary-%s/Release", component, arch)

			if meta, exists := metafiles[packageBase]; exists {
				filenames[packageBase] = meta
			}
			if meta, exists := metafiles[packageBase+".gz"]; exists {
				filenames[packageBase+".gz"] = meta
			}
			if meta, exists := metafiles[packageBase+".xz"]; exists {
				filenames[packageBase+".xz"] = meta
			}
			if meta, exists := metafiles[releaseFile]; exists {
				filenames[releaseFile] = meta
			}
		}
	}

	checkSpecial := make(map[string]distMetafile)
	for filename, meta := range filenames {
		dest := fmt.Sprintf("%s/dists/%s/%s", r.conf.URL, r.conf.Dist, filename)
		size, err := r.downloadRemoteFile(dest)
		if err != nil {
			if !strings.HasSuffix(filename, "Packages") {
				return err
			}
			checkSpecial[dest] = meta
			continue
		}

		if size != meta.size {
			log.Printf("WARNING: Package file not correct size: %d != %d\n", meta.size, size)
		}

		truedest := filepath.Join(r.DownloadRoot, dest)
		if !verifySHA256File(truedest, meta.sha256) {
			log.Printf("WARNING: SHA256 mismatch %s\n", filename)
		}
	}

	for filename, meta := range checkSpecial {
		log.Printf("Processing special file %s\n", filename)
		if err := r.checkCompressedFile(filename, meta); err != nil {
			return err
		}
	}

	return nil
}

func (r *repo) checkCompressedFile(pfile string, meta distMetafile) error {
	fname := filepath.Join(r.DownloadRoot, pfile+".gz")
	if !fileExists(fname) {
		return errors.New("Failed to check " + pfile)
	}

	packageFile, err := os.OpenFile(fname, os.O_RDONLY, 0644)
	if err != nil {
		return err
	}

	gz, err := gzip.NewReader(packageFile)
	if err != nil {
		packageFile.Close()
		return err
	}

	if !verifySHA256Reader(gz, meta.sha256) {
		log.Printf("WARNING: SHA256 mismatch %s\n", pfile)
	}
	gz.Close()
	packageFile.Close()
	return nil
}

func (r *repo) processPackages() (map[string]distMetafile, error) {
	files := make(map[string]distMetafile)

	for _, component := range r.conf.Components {
		for _, arch := range r.conf.Archs {
			packageBase := fmt.Sprintf("%s/binary-%s/Packages", component, arch)
			packageFile := fmt.Sprintf("%s/%s/dists/%s/%s", r.DownloadRoot, r.conf.URL, r.conf.Dist, packageBase)

			packages, err := r.processPackageFile(packageFile)
			if err != nil {
				return nil, err
			}

			for f, m := range packages {
				files[f] = m
			}
		}
	}

	return files, nil
}

func (r *repo) processPackageFile(filename string) (map[string]distMetafile, error) {
	gzipped := false
	if !fileExists(filename) {
		filename = filename + ".gz"
		gzipped = true

		if !fileExists(filename) {
			return nil, errors.New("Cannot find Packages file")
		}
	}

	release, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer release.Close()

	files := make(map[string]distMetafile)

	var reader *bufio.Scanner

	if gzipped {
		gz, err := gzip.NewReader(release)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = bufio.NewScanner(gz)
	} else {
		reader = bufio.NewScanner(release)
	}

	currentFileName := ""
	currentFile := distMetafile{}
	for reader.Scan() {
		line := reader.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			files[currentFileName] = currentFile
			currentFile = distMetafile{}
			currentFileName = ""
			continue
		}

		if line[0] == ' ' { // Skip description continuation lines
			continue
		}

		keyValue := bytes.SplitN(line, []byte{':'}, 2)
		key := keyValue[0]
		value := string(bytes.TrimSpace(keyValue[1]))

		if bytes.Equal(key, []byte("Filename")) {
			currentFileName = value
		} else if bytes.Equal(key, []byte("Size")) {
			size, _ := strconv.ParseInt(value, 10, 64)
			currentFile.size = size
		} else if bytes.Equal(key, []byte("SHA256")) {
			currentFile.sha256 = value
		}
	}

	return files, nil
}

func (r *repo) downloadPackages(files map[string]distMetafile, workers uint) error {
	send := make(chan distMetafile, workers*2)
	var wg sync.WaitGroup
	for i := uint(0); i < workers; i++ {
		wg.Add(1)
		go func() {
			r.downloadFileWorker(send)
			wg.Done()
		}()
	}

	for name, meta := range files {
		meta.filename = name
		send <- meta
	}
	close(send)

	wg.Wait()
	return nil
}

func (r *repo) downloadFileWorker(jobs <-chan distMetafile) {
	for {
		job, open := <-jobs
		if !open {
			break
		}

		dest := filepath.Join(r.conf.URL, job.filename)
		size, err := r.downloadRemoteFile(dest)
		if err != nil {
			log.Println(err)
		}

		if size != job.size {
			log.Printf("WARNING: Incorrect size: %s", job.filename)
		}

		if !verifySHA256File(filepath.Join(r.DownloadRoot, dest), job.sha256) {
			log.Printf("WARNING: Incorrect hash: %s", job.filename)
		}
	}
}

func (r *repo) downloadRemoteFile(dest string) (int64, error) {
	remote := r.conf.Proto + "://" + dest
	dest = filepath.Join(r.DownloadRoot, dest)

	if err := checkAndMakeFilePath(dest); err != nil {
		return 0, err
	}

	if !strings.HasPrefix(remote, "http://") && !strings.HasPrefix(remote, "https://") {
		remote = "http://" + remote
	}

	logDebugf("Downloading %s\n", remote)

	resp, err := httpClient.Get(remote)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return 0, errors.New(resp.Status)
	}

	destfile, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return 0, err
	}
	defer destfile.Close()

	read, err := io.Copy(destfile, resp.Body)
	logDebugf("Downloaded %d bytes\n", read)
	return read, err
}
