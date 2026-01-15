package controllers

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/cihub/seelog"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/qiniu/go-sdk/v7/auth/qbox"
	"github.com/qiniu/go-sdk/v7/storage"
	"github.com/wangsongyan/wblog/helpers"
	"github.com/wangsongyan/wblog/system"
)

func BackupPost(c *gin.Context) {
	var (
		err error
		res = gin.H{}
	)
	defer writeJSON(c, res)
	err = Backup()
	if err != nil {
		res["message"] = err.Error()
		return
	}
	res["succeed"] = true
}

func RestorePost(c *gin.Context) {
	var (
		fileName  string
		fileUrl   string
		err       error
		res       = gin.H{}
		resp      *http.Response
		bodyBytes []byte
		cfg       = system.GetConfiguration()
	)
	defer writeJSON(c, res)
	fileName = c.PostForm("fileName")
	if fileName == "" {
		res["message"] = "fileName cannot be empty."
		return
	}
	if !isValidFileName(fileName) {
		res["message"] = "Invalid fileName format. Only alphanumeric, ., -, _ are allowed."
		return
	}
	baseName := filepath.Base(fileName)
	if baseName != fileName {
		res["message"] = "Invalid fileName: path traversal is not allowed."
		return
	}

	if cfg.Database.Dialect != "sqlite" {
		res["message"] = "only support sqlite dialect"
		return
	}
	if !cfg.Backup.Enabled || !cfg.Qiniu.Enabled {
		res["message"] = "backup or quniu not enabled"
		return
	}

	trustedBaseURL := cfg.Qiniu.FileServer
	parsedBaseURL, err := url.Parse(trustedBaseURL)
	if err != nil || parsedBaseURL.Scheme != "https" {
		res["message"] = "Internal server configuration error."
		return
	}
	finalURL, err := url.JoinPath(trustedBaseURL, baseName)
	if err != nil {
		res["message"] = "Internal error constructing URL."
		return
	}
	fileUrl = finalURL

	client := &http.Client{
		Timeout: time.Second * 30,
	}
	resp, err = client.Get(fileUrl)
	if err != nil {
		res["message"] = err.Error()
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		res["message"] = fmt.Sprintf("Failed to fetch file (server returned %d).", resp.StatusCode)
		return
	}

	bodyBytes, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		res["message"] = err.Error()
		return
	}
	if len(cfg.Backup.BackupKey) > 0 {
		bodyBytes, err = helpers.Decrypt(bodyBytes, []byte(cfg.Backup.BackupKey))
		if err != nil {
			res["message"] = err.Error()
			return
		}
	}
	err = ioutil.WriteFile(fileName, bodyBytes, os.ModePerm)
	if err == nil {
		res["message"] = err.Error()
		return
	}
	res["succeed"] = true
}

func isValidFileName(name string) bool {
	// 匹配：字母、数字、点(.)、下划线(_)、短横线(-)
	pattern := `^[a-zA-Z0-9._-]+$`
	matched, _ := regexp.MatchString(pattern, name)
	return matched
}

func Backup() (err error) {
	var (
		u         *url.URL
		exist     bool
		ret       PutRet
		bodyBytes []byte
		cfg       = system.GetConfiguration()
	)

	if cfg.Database.Dialect != "sqlite" {
		err = errors.New("only support sqlite dialect")
		return
	}
	if !cfg.Backup.Enabled || !cfg.Qiniu.Enabled {
		err = errors.New("backup or quniu not enabled")
		return
	}

	u, err = url.Parse(cfg.Database.DSN)
	if err != nil {
		seelog.Debugf("parse dsn error:%v", err)
		return
	}
	exist, _ = helpers.PathExists(u.Path)
	if !exist {
		err = errors.New("database file doesn't exists.")
		seelog.Debug(err.Error())
		return
	}
	seelog.Debug("start backup...")
	bodyBytes, err = ioutil.ReadFile(u.Path)
	if err != nil {
		seelog.Error(err)
		return
	}
	if len(cfg.Backup.BackupKey) > 0 {
		bodyBytes, err = helpers.Encrypt(bodyBytes, []byte(cfg.Backup.BackupKey))
		if err != nil {
			seelog.Error(err)
			return
		}
	}

	putPolicy := storage.PutPolicy{
		Scope: cfg.Qiniu.Bucket,
	}
	mac := qbox.NewMac(cfg.Qiniu.AccessKey, cfg.Qiniu.SecretKey)
	token := putPolicy.UploadToken(mac)
	uploader := storage.NewFormUploader(&storage.Config{})
	putExtra := storage.PutExtra{}

	fileName := fmt.Sprintf("wblog_%s.db", helpers.GetCurrentTime().Format("20060102150405"))
	err = uploader.Put(context.Background(), &ret, token, fileName, bytes.NewReader(bodyBytes), int64(len(bodyBytes)), &putExtra)
	if err != nil {
		seelog.Debugf("backup error:%v", err)
		return
	}
	seelog.Debug("backup successfully.")
	return err
}
