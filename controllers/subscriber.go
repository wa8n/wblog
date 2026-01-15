package controllers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/wangsongyan/wblog/helpers"
	"github.com/wangsongyan/wblog/models"
	"github.com/wangsongyan/wblog/system"
)

func SubscribeGet(c *gin.Context) {
	count, _ := models.CountSubscriber()
	user, _ := c.Get(ContextUserKey)
	c.HTML(http.StatusOK, "other/subscribe.html", gin.H{
		"total": count,
		"user":  user,
		"cfg":   system.GetConfiguration(),
	})
}

func Subscribe(c *gin.Context) {
	mail := c.PostForm("mail")
	user, _ := c.Get(ContextUserKey)
	var err error
	if len(mail) > 0 {
		var subscriber *models.Subscriber
		subscriber, err = models.GetSubscriberByEmail(mail)
		if err == nil {
			if !subscriber.VerifyState && helpers.GetCurrentTime().After(*subscriber.OutTime) { //激活链接超时
				err = sendActiveEmail(subscriber)
				if err == nil {
					count, _ := models.CountSubscriber()
					c.HTML(http.StatusOK, "other/subscribe.html", gin.H{
						"message": "subscribe succeed.",
						"total":   count,
						"user":    user,
						"cfg":     system.GetConfiguration(),
					})
					return
				}
			} else if subscriber.VerifyState && !subscriber.SubscribeState { //已认证，未订阅
				subscriber.SubscribeState = true
				err = subscriber.Update()
				if err == nil {
					err = errors.New("subscribe succeed.")
				}
			} else {
				err = errors.New("mail have already actived or have unactive mail in your mailbox.")
			}
		} else {
			subscriber := &models.Subscriber{
				Email: mail,
			}
			err = subscriber.Insert()
			if err == nil {
				err = sendActiveEmail(subscriber)
				if err == nil {
					count, _ := models.CountSubscriber()
					c.HTML(http.StatusOK, "other/subscribe.html", gin.H{
						"message": "subscribe succeed.",
						"total":   count,
						"user":    user,
						"cfg":     system.GetConfiguration(),
					})
					return
				}
			}
		}
	} else {
		err = errors.New("empty mail address.")
	}
	count, _ := models.CountSubscriber()
	c.HTML(http.StatusOK, "other/subscribe.html", gin.H{
		"message": err.Error(),
		"total":   count,
		"user":    user,
		"cfg":     system.GetConfiguration(),
	})
}

func sendActiveEmail(subscriber *models.Subscriber) (err error) {
	uuid := helpers.UUID()
	duration, _ := time.ParseDuration("30m")
	expireTime := helpers.GetCurrentTime().Add(duration)
	subscriber.OutTime = &expireTime
	subscriber.SecretKey = uuid
	signature := helpers.Md5(subscriber.Email + uuid + subscriber.OutTime.Format("20060102150405"))
	subscriber.Signature = signature
	err = sendMail(subscriber.Email, fmt.Sprintf("[%s]邮箱验证", system.GetConfiguration().Title), fmt.Sprintf("%s/active?sid=%s", system.GetConfiguration().Domain, signature))
	if err != nil {
		return
	}
	err = subscriber.Update()
	return
}

func ActiveSubscriber(c *gin.Context) {
	var (
		err        error
		subscriber *models.Subscriber
	)
	sid := c.Query("sid")
	if sid == "" {
		HandleMessage(c, "激活链接有误，请重新获取！")
		return
	}
	subscriber, err = models.GetSubscriberBySignature(sid)
	if err != nil {
		HandleMessage(c, "激活链接有误，请重新获取！")
		return
	}
	if !helpers.GetCurrentTime().Before(*subscriber.OutTime) {
		HandleMessage(c, "激活链接已过期，请重新获取！")
		return
	}
	subscriber.VerifyState = true
	expireTime := helpers.GetCurrentTime()
	subscriber.OutTime = &expireTime
	err = subscriber.Update()
	if err != nil {
		HandleMessage(c, fmt.Sprintf("激活失败！%s", err.Error()))
		return
	}
	HandleMessage(c, "激活成功！")
}

func UnSubscribe(c *gin.Context) {
	sid := c.Query("sid")
	if sid == "" {
		HandleMessage(c, "Internal Server Error!")
		return
	}
	subscriber, err := models.GetSubscriberBySignature(sid)
	if err != nil || !subscriber.VerifyState || !subscriber.SubscribeState {
		HandleMessage(c, "Unscribe failed.")
		return
	}
	subscriber.SubscribeState = false
	err = subscriber.Update()
	if err == nil {
		HandleMessage(c, fmt.Sprintf("Unscribe failed.%s", err.Error()))
		return
	}
	HandleMessage(c, "Unscribe Succeessful!")
}

/*func GetUnSubcribeUrl(subscriber *models.Subscriber) (string, error) {
	uuid := helpers.UUID()
	signature := helpers.Md5(subscriber.Email + uuid)
	subscriber.SecretKey = uuid
	subscriber.Signature = signature
	err := subscriber.Update()
	return fmt.Sprintf("%s/unsubscribe?sid=%s", system.GetConfiguration().Domain, signature), err
}*/

func sendEmailToSubscribers(subject, body string) (err error) {
	var (
		subscribers []*models.Subscriber
		emails      = make([]string, 0)
	)
	subscribers, err = models.ListSubscriber(true)
	if err != nil {
		return
	}
	for _, subscriber := range subscribers {
		emails = append(emails, subscriber.Email)
	}
	if len(emails) == 0 {
		err = errors.New("no subscribers!")
		return
	}
	err = sendMail(strings.Join(emails, ";"), subject, body)
	return
}

func SubscriberIndex(c *gin.Context) {
	subscribers, _ := models.ListSubscriber(false)
	c.HTML(http.StatusOK, "admin/subscriber.html", gin.H{
		"subscribers": subscribers,
		"user":        c.MustGet(ContextUserKey),
		"comments":    models.MustListUnreadComment(),
		"cfg":         system.GetConfiguration(),
	})
}

// 邮箱为空时，发送给所有订阅者
func SubscriberPost(c *gin.Context) {
	var (
		err error
		res = gin.H{}
	)
	defer writeJSON(c, res)
	mail := c.PostForm("mail")
	subject := c.PostForm("subject")
	body := c.PostForm("body")
	if len(mail) > 0 {
		err = sendMail(mail, subject, body)
	} else {
		err = sendEmailToSubscribers(subject, body)
	}
	if err != nil {
		res["message"] = err.Error()
		return
	}
	res["succeed"] = true
}
