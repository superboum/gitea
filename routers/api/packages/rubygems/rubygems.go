// Copyright 2021 The Gitea Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package rubygems

import (
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"net/http"
	"strings"

	packages_model "code.gitea.io/gitea/models/packages"
	"code.gitea.io/gitea/modules/context"
	packages_module "code.gitea.io/gitea/modules/packages"
	rubygems_module "code.gitea.io/gitea/modules/packages/rubygems"
	"code.gitea.io/gitea/routers/api/packages/helper"
	packages_service "code.gitea.io/gitea/services/packages"
)

func apiError(ctx *context.Context, status int, obj interface{}) {
	helper.LogAndProcessError(ctx, status, obj, func(message string) {
		ctx.PlainText(status, message)
	})
}

// EnumeratePackages serves the package list
func EnumeratePackages(ctx *context.Context) {
	packages, err := packages_model.GetVersionsByPackageType(ctx, ctx.Package.Owner.ID, packages_model.TypeRubyGems)
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	enumeratePackages(ctx, "specs.4.8", packages)
}

// EnumeratePackagesLatest serves the list of the lastest version of every package
func EnumeratePackagesLatest(ctx *context.Context) {
	pvs, _, err := packages_model.SearchLatestVersions(ctx, &packages_model.PackageSearchOptions{
		OwnerID: ctx.Package.Owner.ID,
		Type:    packages_model.TypeRubyGems,
	})
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	enumeratePackages(ctx, "latest_specs.4.8", pvs)
}

// EnumeratePackagesPreRelease is not supported and serves an empty list
func EnumeratePackagesPreRelease(ctx *context.Context) {
	enumeratePackages(ctx, "prerelease_specs.4.8", []*packages_model.PackageVersion{})
}

func enumeratePackages(ctx *context.Context, filename string, pvs []*packages_model.PackageVersion) {
	pds, err := packages_model.GetPackageDescriptors(ctx, pvs)
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	specs := make([]interface{}, 0, len(pds))
	for _, p := range pds {
		specs = append(specs, []interface{}{
			p.Package.Name,
			&rubygems_module.RubyUserMarshal{
				Name:  "Gem::Version",
				Value: []string{p.Version.Version},
			},
			p.Metadata.(*rubygems_module.Metadata).Platform,
		})
	}

	ctx.SetServeHeaders(&context.ServeHeaderOptions{
		Filename: filename + ".gz",
	})

	zw := gzip.NewWriter(ctx.Resp)
	defer zw.Close()

	zw.Name = filename

	if err := rubygems_module.NewMarshalEncoder(zw).Encode(specs); err != nil {
		ctx.ServerError("Download file failed", err)
	}
}

// ServePackageSpecification serves the compressed Gemspec file of a package
func ServePackageSpecification(ctx *context.Context) {
	filename := ctx.Params("filename")

	if !strings.HasSuffix(filename, ".gemspec.rz") {
		apiError(ctx, http.StatusNotImplemented, nil)
		return
	}

	pvs, err := getVersionsByFilename(ctx, filename[:len(filename)-10]+"gem")
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	if len(pvs) != 1 {
		apiError(ctx, http.StatusNotFound, nil)
		return
	}

	pd, err := packages_model.GetPackageDescriptor(ctx, pvs[0])
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	ctx.SetServeHeaders(&context.ServeHeaderOptions{
		Filename: filename,
	})

	zw := zlib.NewWriter(ctx.Resp)
	defer zw.Close()

	metadata := pd.Metadata.(*rubygems_module.Metadata)

	// create a Ruby Gem::Specification object
	spec := &rubygems_module.RubyUserDef{
		Name: "Gem::Specification",
		Value: []interface{}{
			"3.2.3", // @rubygems_version
			4,       // @specification_version,
			pd.Package.Name,
			&rubygems_module.RubyUserMarshal{
				Name:  "Gem::Version",
				Value: []string{pd.Version.Version},
			},
			nil,               // date
			metadata.Summary,  // @summary
			nil,               // @required_ruby_version
			nil,               // @required_rubygems_version
			metadata.Platform, // @original_platform
			[]interface{}{},   // @dependencies
			nil,               // rubyforge_project
			"",                // @email
			metadata.Authors,
			metadata.Description,
			metadata.ProjectURL,
			true,              // has_rdoc
			metadata.Platform, // @new_platform
			nil,
			metadata.Licenses,
		},
	}

	if err := rubygems_module.NewMarshalEncoder(zw).Encode(spec); err != nil {
		ctx.ServerError("Download file failed", err)
	}
}

// DownloadPackageFile serves the content of a package
func DownloadPackageFile(ctx *context.Context) {
	filename := ctx.Params("filename")

	pvs, err := getVersionsByFilename(ctx, filename)
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	if len(pvs) != 1 {
		apiError(ctx, http.StatusNotFound, nil)
		return
	}

	s, pf, err := packages_service.GetFileStreamByPackageVersion(
		ctx,
		pvs[0],
		&packages_service.PackageFileInfo{
			Filename: filename,
		},
	)
	if err != nil {
		if err == packages_model.ErrPackageFileNotExist {
			apiError(ctx, http.StatusNotFound, err)
			return
		}
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}
	defer s.Close()

	ctx.ServeStream(s, pf.Name)
}

// UploadPackageFile adds a file to the package. If the package does not exist, it gets created.
func UploadPackageFile(ctx *context.Context) {
	upload, close, err := ctx.UploadStream()
	if err != nil {
		apiError(ctx, http.StatusBadRequest, err)
		return
	}
	if close {
		defer upload.Close()
	}

	buf, err := packages_module.CreateHashedBufferFromReader(upload, 32*1024*1024)
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}
	defer buf.Close()

	rp, err := rubygems_module.ParsePackageMetaData(buf)
	if err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}
	if _, err := buf.Seek(0, io.SeekStart); err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	var filename string
	if rp.Metadata.Platform == "" || rp.Metadata.Platform == "ruby" {
		filename = strings.ToLower(fmt.Sprintf("%s-%s.gem", rp.Name, rp.Version))
	} else {
		filename = strings.ToLower(fmt.Sprintf("%s-%s-%s.gem", rp.Name, rp.Version, rp.Metadata.Platform))
	}

	_, _, err = packages_service.CreatePackageAndAddFile(
		&packages_service.PackageCreationInfo{
			PackageInfo: packages_service.PackageInfo{
				Owner:       ctx.Package.Owner,
				PackageType: packages_model.TypeRubyGems,
				Name:        rp.Name,
				Version:     rp.Version,
			},
			SemverCompatible: true,
			Creator:          ctx.Doer,
			Metadata:         rp.Metadata,
		},
		&packages_service.PackageFileCreationInfo{
			PackageFileInfo: packages_service.PackageFileInfo{
				Filename: filename,
			},
			Data:   buf,
			IsLead: true,
		},
	)
	if err != nil {
		if err == packages_model.ErrDuplicatePackageVersion {
			apiError(ctx, http.StatusBadRequest, err)
			return
		}
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}

	ctx.Status(http.StatusCreated)
}

// DeletePackage deletes a package
func DeletePackage(ctx *context.Context) {
	// Go populates the form only for POST, PUT and PATCH requests
	if err := ctx.Req.ParseMultipartForm(32 << 20); err != nil {
		apiError(ctx, http.StatusInternalServerError, err)
		return
	}
	packageName := ctx.FormString("gem_name")
	packageVersion := ctx.FormString("version")

	err := packages_service.RemovePackageVersionByNameAndVersion(
		ctx.Doer,
		&packages_service.PackageInfo{
			Owner:       ctx.Package.Owner,
			PackageType: packages_model.TypeRubyGems,
			Name:        packageName,
			Version:     packageVersion,
		},
	)
	if err != nil {
		if err == packages_model.ErrPackageNotExist {
			apiError(ctx, http.StatusNotFound, err)
			return
		}
		apiError(ctx, http.StatusInternalServerError, err)
	}
}

func getVersionsByFilename(ctx *context.Context, filename string) ([]*packages_model.PackageVersion, error) {
	pvs, _, err := packages_model.SearchVersions(ctx, &packages_model.PackageSearchOptions{
		OwnerID:         ctx.Package.Owner.ID,
		Type:            packages_model.TypeRubyGems,
		HasFileWithName: filename,
	})
	return pvs, err
}
