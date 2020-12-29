package pending_review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// Review States
	CHANGE    = "CHANGES_REQUESTED"
	APPRVD    = "APPROVED"
	DISMISSED = "DISMISSED"

	// Author Associations
	COLLABORATOR = "COLLABORATOR"
	CONTRIBUTOR  = "CONTRIBUTOR"
	MEMBER       = "MEMBER"
)

type PullRequestService service

type PullRequestStatus struct {
	Number              int
	OpenedBy            string
	Title               string
	ReviewURL           string
	LastCommitSHA       string
	LastCommitAt        time.Time
	Reviews             int
	AtLeastOneApproval  bool
	HeadCommitApprovals []string
	HeadCommitBlockers  []string
}

var ErrNoReviews = errors.New("no reviews on pull request")

func (s *PullRequestService) GatherRelevantReviews(ctx context.Context, owner string, repo string, pr *PullRequest) (*PullRequestStatus, *Response, error) {
	p := &PullRequestStatus{
		Number:        pr.GetNumber(),
		OpenedBy:      pr.GetUser().GetLogin(),
		ReviewURL:     pr.GetHTMLURL(),
		LastCommitSHA: pr.GetHead().GetSHA(),
	}

	files, resp, err := s.client.PullRequests.ListFiles(ctx, owner, repo, p.Number, &ListOptions{
		Page:    0,
		PerPage: 5,
	})
	if err != nil {
		return nil, resp, err
	}

	p.Title = strings.SplitN(files[0].GetFilename(), "/", 3)[1] // FIXME: Error handling
	if files[0].GetStatus() == "added" {
		p.Title = ":new: " + p.Title
	} else {
		p.Title = ":memo: " + p.Title
	}

	opt := &ListOptions{
		Page:    0,
		PerPage: 100,
	}
	for {
		reviews, resp, err := s.client.PullRequests.ListReviews(ctx, owner, repo, p.Number, opt)
		if err != nil {
			return nil, resp, err
		}

		if p.Reviews = len(reviews); p.Reviews < 1 { // Has not been looked at...
			commit, resp, err := s.client.Repositories.GetCommit(ctx, pr.GetHead().GetRepo().GetOwner().GetLogin(), pr.GetHead().GetRepo().GetName(), p.LastCommitSHA)
			if err != nil {
				return nil, resp, err
			}
			p.LastCommitAt = commit.GetCommit().GetAuthor().GetDate()

			hours, _ := time.ParseDuration("24h")
			if p.LastCommitAt.Add(hours).After(time.Now()) { // commited within 24hrs
				return p, resp, nil // let's save it!
			}
			return nil, resp, fmt.Errorf("%w", ErrNoReviews)
		}

		for _, review := range reviews {
			onBranchHead := p.LastCommitSHA == review.GetCommitID()
			reviewerName := review.User.GetLogin()
			reviewerAssociation := review.GetAuthorAssociation()
			isC3iTeam := reviewerAssociation == MEMBER || reviewerAssociation == COLLABORATOR

			switch state := review.GetState(); state {
			case CHANGE:
				if isC3iTeam {
					p.HeadCommitBlockers = appendUnique(p.HeadCommitBlockers, reviewerName)
				}

				p.HeadCommitApprovals = removeUnique(p.HeadCommitApprovals, reviewerName)
			case APPRVD:
				p.AtLeastOneApproval = true
				if onBranchHead {
					p.HeadCommitApprovals = appendUnique(p.HeadCommitApprovals, reviewerName)
				}

				p.HeadCommitBlockers = removeUnique(p.HeadCommitBlockers, reviewerName)
			case DISMISSED:
				p.HeadCommitBlockers = removeUnique(p.HeadCommitBlockers, reviewerName)
			default:
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	if p.AtLeastOneApproval {
		return p, resp, nil
	}

	return nil, resp, fmt.Errorf("%w", ErrNoReviews)
}

func appendUnique(slice []string, elem string) []string {
	for _, e := range slice {
		if e == elem {
			return slice
		}
	}

	return append(slice, elem)
}

func removeUnique(slice []string, elem string) []string {
	for i, e := range slice {
		if e == elem {
			return append(slice[:i], slice[i+1:]...)
		}
	}

	return slice
}
