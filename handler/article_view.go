package handler

import (
	"fmt"
	"html/template"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/coyove/iis/common"
	"github.com/coyove/iis/dal"
	"github.com/coyove/iis/ik"
	"github.com/coyove/iis/model"
	"github.com/gin-gonic/gin"
)

type ArticleView struct {
	ID            string
	Link          string
	ParentLink    string
	Others        []*ArticleView
	Parent        *ArticleView
	Author        *model.User
	You           *model.User
	Cmd           string
	Replies       int
	Likes         int
	ReplyLockMode byte
	Liked         bool
	NSFW          bool
	NoAvatar      bool
	GreyOutReply  bool
	AlsoReply     bool
	StickOnTop    bool
	Forward       bool
	AscReply      bool
	OpenBlank     bool
	IsCrawler     bool
	Content       string
	ShortContent  string
	ContentHTML   template.HTML
	Media         template.HTML
	MediaType     string
	History       string
	CreateTime    time.Time
	Extras        map[string]string
}

const (
	aTimeline      = 0
	aTimelineReply = 1
	aReplyParent   = 2
	aReply         = 3
)

func (a *ArticleView) from(a2 *model.Article, opt int, u *model.User) *ArticleView {
	if a2 == nil {
		return a
	}

	a.ID = a2.ID
	a.Link = "/S/" + a.ID[1:]
	a.Replies = int(a2.Replies)
	a.Likes = int(a2.Likes)
	a.ReplyLockMode = a2.ReplyLockMode
	a.NSFW = a2.NSFW
	a.StickOnTop = a2.T_StickOnTop
	a.Cmd = string(a2.Cmd)
	a.CreateTime = a2.CreateTime
	a.History = a2.History
	a.Extras = a2.Extras
	a.AscReply = a2.Asc == 1

	a.Author, _ = dal.WeakGetUser(a2.Author)
	if a.Author == nil {
		a.Author = (&model.User{ID: a2.Author}).SetInvalid()
	}
	if a2.Anonymous {
		a.Author.SetIsAnon(true)
		a.Author.ID = fmt.Sprintf("=%d=", time.Now().UnixNano()/1e3)
		a.Author.Avatar = 0
	}
	if a2.Extras["is_bot"] != "" {
		a.Author.SetIsAPI(true)
	}

	a.You = u
	if a.You == nil {
		a.You = &model.User{}
	} else {
		a.Liked = dal.IsLiking(u.ID, a2.ID)

		if a.Extras["poll_title"] != "" {
			pa, err := dal.GetArticle("u/" + u.ID + "/poll/" + a.ID)
			if err == nil {
				a.Extras["poll_you_voted"] = pa.Extras["choice"]
				for i := 1; i < 8; i++ {
					if k := "poll_" + strconv.Itoa(i); a.Extras[k] == "" {
						a.Extras[k] = "0"
					}
				}
			}
			ttl := common.ParseDuration(a.Extras["poll_live"])
			if ttl > 0 {
				if a.CreateTime.Add(ttl).Before(time.Now()) {
					a.Extras["poll_closed"] = "1"
				}
				a.Extras["poll_deadline"] = a.CreateTime.Add(ttl).Format("2006-01-02 15:04")
			}
		}
	}

	if p := strings.SplitN(a2.Media, ":", 2); len(p) == 2 {
		a.MediaType = p[0]
		switch a.MediaType {
		case "IMG":
			var pl struct {
				Urls     []string
				InitShow int
				NSFW     bool
			}
			for _, p := range strings.Split(p[1], ";") {
				pl.Urls = append(pl.Urls, common.Cfg.MediaDomain+"/"+strings.TrimPrefix(p, "LOCAL:"))
				if len(pl.Urls) == 15 {
					break
				}
			}
			if pl.NSFW = a.NSFW; pl.NSFW {
				pl.InitShow = 0
			} else {
				pl.InitShow = 4
			}
			if a.You.FoldAllImages == 1 {
				pl.InitShow = 0
			} else if a.You.ExpandNSFWImages == 1 {
				pl.InitShow = 4
			}
			a.Media = template.HTML(common.RevRenderTemplateString("image.html", pl))
		}
	}

	a.Content = a2.Content
	a.ContentHTML = a2.ContentHTML()
	a.Forward = a.Content == "" && a.MediaType == ""

	if a2.Parent != "" {
		a.Parent = &ArticleView{}
		if opt == aTimeline {
			p, _ := dal.WeakGetArticle(a2.Parent)
			a.Parent.from(p, aTimelineReply, u)
		}
		a.ParentLink = "/S/" + a2.Parent[1:]
	}

	a.GreyOutReply = opt == aReplyParent
	a.NoAvatar = opt == aTimelineReply
	a.OpenBlank = opt != aReply

	switch a2.Cmd {
	case model.CmdInboxReply, model.CmdInboxMention:
		p, _ := dal.WeakGetArticle(a2.Extras["article_id"])
		if p == nil {
			*a = ArticleView{}
			return a
		}

		a.from(p, opt, u)
		a.Cmd = string(a2.Cmd)
	case model.CmdInboxFwAccepted:
		dummy := &model.Article{
			ID:         ik.NewGeneralID().String(),
			CreateTime: a2.CreateTime,
			Author:     a2.Extras["from"],
		}
		a.from(dummy, opt, u)
		a.Cmd = model.CmdInboxFwAccepted
	case model.CmdInboxFwApply:
		following, accepted := dal.IsFollowingWithAcceptance(a2.Extras["from"], u)
		if following && accepted {
			*a = ArticleView{}
			return a
		}

		dummy := &model.Article{
			ID:         ik.NewGeneralID().String(),
			CreateTime: a2.CreateTime,
			Author:     a2.Extras["from"],
		}
		a.from(dummy, opt, u)
		a.Cmd = model.CmdInboxFwApply
	case model.CmdInboxLike, model.CmdTimelineLike:
		p, _ := dal.WeakGetArticle(a2.Extras["article_id"])
		if p == nil {
			*a = ArticleView{}
			return a
		}

		if !dal.IsLiking(a2.Extras["from"], p.ID) {
			*a = ArticleView{}
			return a
		}

		dummy := &model.Article{
			ID:         ik.NewGeneralID().String(),
			CreateTime: a2.CreateTime,
			Author:     a2.Extras["from"],
			Parent:     p.ID,
		}
		a.from(dummy, opt, u)
		a.Cmd = string(a2.Cmd)
	default:
		if u != nil &&
			(dal.IsBlocking(u.ID, a.Author.ID) || dal.IsBlocking(a.Author.ID, u.ID)) {
			*a = ArticleView{}
		}
	}

	return a
}

func fromMultiple(g *gin.Context, a *[]ArticleView, a2 []*model.Article, opt int, u *model.User) {
	*a = make([]ArticleView, len(a2))

	lookup := map[string]*ArticleView{}
	dedup := map[string]bool{}
	cluster := map[string][]string{}
	isCrawler := common.IsCrawler(g)

	for i, v := range a2 {
		(*a)[i].from(v, opt, u)
		(*a)[i].IsCrawler = isCrawler
		lookup[(*a)[i].ID] = &(*a)[i]
	}

	// First pass, connect continous replies (r1 -> r2 -> ... -> first_post) into one post
	for i, v := range *a {
		if v.Parent != nil {
			cluster[v.Parent.ID] = append(cluster[v.Parent.ID], v.ID) // will be used in second pass

			if lookup[v.Parent.ID] != nil {
				p := lookup[v.Parent.ID]
				p.NoAvatar = true
				(*a)[i].Parent = p
				dedup[p.ID] = true
			}
		}
	}

	if opt == aTimeline {
		// Second pass, connect clustered replies (r1 -> p, r2 -> p ...., rn -> p) into one post
	NEXT:
		for _, rids := range cluster {
			if len(rids) == 1 {
				continue
			}
			for _, rid := range rids {
				if dedup[rid] {
					continue NEXT
				}
			}
			// All posts are visible
			r := lookup[rids[0]]
			for i := 1; i < len(rids); i++ {
				ri := lookup[rids[i]]
				if ri == nil {
					log.Println("[ClusterReplies] nil:", rids[i])
					continue
				}
				ri.Parent = nil
				ri.Others = nil
				ri.AlsoReply = true
				r.Others = append(r.Others, ri)
				dedup[rids[i]] = true
			}
		}
	}

	newa := make([]ArticleView, 0, len(*a))
	for _, v := range *a {
		if dedup[v.ID] {
			continue
		}
		if v.ID == "" {
			continue
		}
		newa = append(newa, v)
	}
	*a = newa
}
