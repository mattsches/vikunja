// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public Licensee as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public Licensee for more details.
//
// You should have received a copy of the GNU Affero General Public Licensee
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package trello

import (
	"bytes"

	"code.vikunja.io/api/pkg/config"
	"code.vikunja.io/api/pkg/files"
	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/modules/migration"
	"code.vikunja.io/api/pkg/user"

	"github.com/adlio/trello"
	"github.com/yuin/goldmark"
)

// Migration represents the trello migration struct
type Migration struct {
	Token string `json:"code"`
}

var trelloColorMap map[string]string

func init() {
	trelloColorMap = make(map[string]string, 30)
	trelloColorMap = map[string]string{
		"green":        "4bce97",
		"yellow":       "f5cd47",
		"orange":       "fea362",
		"red":          "f87168",
		"purple":       "9f8fef",
		"blue":         "579dff",
		"sky":          "6cc3e0",
		"lime":         "94c748",
		"pink":         "e774bb",
		"black":        "8590a2",
		"green_dark":   "1f845a",
		"yellow_dark":  "946f00",
		"orange_dark":  "c25100",
		"red_dark":     "c9372c",
		"purple_dark":  "6e5dc6",
		"blue_dark":    "0c66e4",
		"sky_dark":     "227d9b",
		"lime_dark":    "5b7f24",
		"pink_dark":    "ae4787",
		"black_dark":   "626f86",
		"green_light":  "baf3db",
		"yellow_light": "f8e6a0",
		"orange_light": "fedec8",
		"red_light":    "ffd5d2",
		"purple_light": "dfd8fd",
		"blue_light":   "cce0ff",
		"sky_light":    "c6edfb",
		"lime_light":   "d3f1a7",
		"ping_light":   "fdd0ec",
		"black_light":  "dcdfe4",
		"transparent":  "", // Empty
	}
}

// Name is used to get the name of the trello migration - we're using the docs here to annotate the status route.
// @Summary Get migration status
// @Description Returns if the current user already did the migation or not. This is useful to show a confirmation message in the frontend if the user is trying to do the same migration again.
// @tags migration
// @Produce json
// @Security JWTKeyAuth
// @Success 200 {object} migration.Status "The migration status"
// @Failure 500 {object} models.Message "Internal server error"
// @Router /migration/trello/status [get]
func (m *Migration) Name() string {
	return "trello"
}

// AuthURL returns the url users need to authenticate against
// @Summary Get the auth url from trello
// @Description Returns the auth url where the user needs to get its auth code. This code can then be used to migrate everything from trello to Vikunja.
// @tags migration
// @Produce json
// @Security JWTKeyAuth
// @Success 200 {object} handler.AuthURL "The auth url."
// @Failure 500 {object} models.Message "Internal server error"
// @Router /migration/trello/auth [get]
func (m *Migration) AuthURL() string {
	return "https://trello.com/1/authorize" +
		"?expiration=never" +
		"&scope=read" +
		"&callback_method=fragment" +
		"&response_type=token" +
		"&name=Vikunja%20Migration" +
		"&key=" + config.MigrationTrelloKey.GetString() +
		"&return_url=" + config.MigrationTrelloRedirectURL.GetString()
}

func getTrelloData(token string) (trelloData []*trello.Board, err error) {
	allArg := trello.Arguments{"fields": "all"}

	client := trello.NewClient(config.MigrationTrelloKey.GetString(), token)
	client.Logger = log.GetLogger()

	log.Debugf("[Trello Migration] Getting boards...")

	trelloData, err = client.GetMyBoards(trello.Defaults())
	if err != nil {
		return
	}

	log.Debugf("[Trello Migration] Got %d trello boards", len(trelloData))

	for _, board := range trelloData {
		log.Debugf("[Trello Migration] Getting projects for board %s", board.ID)

		board.Lists, err = board.GetLists(trello.Defaults())
		if err != nil {
			return
		}

		log.Debugf("[Trello Migration] Got %d projects for board %s", len(board.Lists), board.ID)

		listMap := make(map[string]*trello.List, len(board.Lists))
		for _, list := range board.Lists {
			listMap[list.ID] = list
		}

		log.Debugf("[Trello Migration] Getting cards for board %s", board.ID)

		cards, err := board.GetCards(allArg)
		if err != nil {
			return nil, err
		}

		log.Debugf("[Trello Migration] Got %d cards for board %s", len(cards), board.ID)

		for _, card := range cards {
			list, exists := listMap[card.IDList]
			if !exists {
				continue
			}

			card.Attachments, err = card.GetAttachments(allArg)
			if err != nil {
				return nil, err
			}

			if len(card.IDCheckLists) > 0 {
				for _, checkListID := range card.IDCheckLists {
					checklist, err := client.GetChecklist(checkListID, allArg)
					if err != nil {
						return nil, err
					}

					checklist.CheckItems = []trello.CheckItem{}
					err = client.Get("checklists/"+checkListID+"/checkItems", allArg, &checklist.CheckItems)
					if err != nil {
						return nil, err
					}

					card.Checklists = append(card.Checklists, checklist)
					log.Debugf("Retrieved checklist %s for card %s", checkListID, card.ID)
				}
			}

			list.Cards = append(list.Cards, card)
		}

		log.Debugf("[Trello Migration] Looked for attachements on all cards of board %s", board.ID)
	}

	return
}

func convertMarkdownToHTML(input string) (output string, err error) {
	var buf bytes.Buffer
	err = goldmark.Convert([]byte(input), &buf)
	if err != nil {
		return
	}
	//#nosec - we are not responsible to escape this as we don't know the context where it is used
	return buf.String(), nil
}

// Converts all previously obtained data from trello into the vikunja format.
// `trelloData` should contain all boards with their projects and cards respectively.
func convertTrelloDataToVikunja(trelloData []*trello.Board, token string) (fullVikunjaHierachie []*models.ProjectWithTasksAndBuckets, err error) {

	log.Debugf("[Trello Migration] ")

	var pseudoParentID int64 = 1
	fullVikunjaHierachie = []*models.ProjectWithTasksAndBuckets{
		{
			Project: models.Project{
				ID:    pseudoParentID,
				Title: "Imported from Trello",
			},
		},
	}

	var bucketID int64 = 1

	log.Debugf("[Trello Migration] Converting %d boards to vikunja projects", len(trelloData))

	for index, board := range trelloData {
		project := &models.ProjectWithTasksAndBuckets{
			Project: models.Project{
				ID:              int64(index+1) + pseudoParentID,
				ParentProjectID: pseudoParentID,
				Title:           board.Name,
				Description:     board.Desc,
				IsArchived:      board.Closed,
			},
		}

		// Background
		// We're pretty much abusing the backgroundinformation field here - not sure if this is really better than adding a new property to the project
		if board.Prefs.BackgroundImage != "" {
			log.Debugf("[Trello Migration] Downloading background %s for board %s", board.Prefs.BackgroundImage, board.ID)
			buf, err := migration.DownloadFile(board.Prefs.BackgroundImage)
			if err != nil {
				return nil, err
			}
			log.Debugf("[Trello Migration] Downloaded background %s for board %s", board.Prefs.BackgroundImage, board.ID)
			project.BackgroundInformation = buf
		} else {
			log.Debugf("[Trello Migration] Board %s does not have a background image, not copying...", board.ID)
		}

		for _, l := range board.Lists {
			bucket := &models.Bucket{
				ID:    bucketID,
				Title: l.Name,
			}

			log.Debugf("[Trello Migration] Converting %d cards to tasks from board %s", len(l.Cards), board.ID)

			for _, card := range l.Cards {

				log.Debugf("[Trello Migration] Converting card %s", card.ID)

				// The usual stuff: Title, description, position, bucket id
				task := &models.Task{
					Title:    card.Name,
					BucketID: bucketID,
				}

				task.Description, err = convertMarkdownToHTML(card.Desc)
				if err != nil {
					return nil, err
				}

				if card.Due != nil {
					task.DueDate = *card.Due
				}

				// Checklists (as markdown in description)
				for _, checklist := range card.Checklists {
					task.Description += "\n\n<h2> " + checklist.Name + "</h2>\n\n" + `<ul data-type="taskList">`

					for _, item := range checklist.CheckItems {
						task.Description += "\n"
						if item.State == "complete" {
							task.Description += `<li data-checked="true" data-type="taskItem"><label><input type="checkbox" checked="checked"><span></span></label><div><p>` + item.Name + `</p></div></li>`
						} else {
							task.Description += `<li data-checked="false" data-type="taskItem"><label><input type="checkbox"><span></span></label><div><p>` + item.Name + `</p></div></li>`
						}
					}
					task.Description += "</ul>"
				}
				if len(card.Checklists) > 0 {
					log.Debugf("[Trello Migration] Converted %d checklists from card %s", len(card.Checklists), card.ID)
				}

				// Labels
				for _, label := range card.Labels {
					color, exists := trelloColorMap[label.Color]
					if !exists {
						log.Debugf("[Trello Migration] Color %s not mapped for trello card %s, falling back to transparent", label.Color, card.ID)
						color = trelloColorMap["transparent"]
					}

					task.Labels = append(task.Labels, &models.Label{
						Title:    label.Name,
						HexColor: color,
					})

					log.Debugf("[Trello Migration] Converted label %s from card %s", label.ID, card.ID)
				}

				// Attachments
				if len(card.Attachments) > 0 {
					log.Debugf("[Trello Migration] Downloading %d card attachments from card %s", len(card.Attachments), card.ID)
				}
				for _, attachment := range card.Attachments {
					if !attachment.IsUpload { // There are other types of attachments which are not files. We can only handle files.
						log.Debugf("[Trello Migration] Attachment %s does not have a mime type, not downloading", attachment.ID)
						continue
					}

					log.Debugf("[Trello Migration] Downloading card attachment %s", attachment.ID)

					buf, err := migration.DownloadFileWithHeaders(attachment.URL, map[string][]string{
						"Authorization": {`OAuth oauth_consumer_key="` + config.MigrationTrelloKey.GetString() + `", oauth_token="` + token + `"`},
					})
					if err != nil {
						return nil, err
					}

					vikunjaAttachment := &models.TaskAttachment{
						File: &files.File{
							Name:        attachment.Name,
							Mime:        attachment.MimeType,
							Size:        uint64(buf.Len()),
							FileContent: buf.Bytes(),
						},
					}

					if card.IDAttachmentCover != "" && card.IDAttachmentCover == attachment.ID {
						vikunjaAttachment.ID = 42
						task.CoverImageAttachmentID = 42
					}

					task.Attachments = append(task.Attachments, vikunjaAttachment)

					log.Debugf("[Trello Migration] Downloaded card attachment %s", attachment.ID)
				}

				// When the cover image was set manually, we need to add it as an attachment
				if card.ManualCoverAttachment && len(card.Cover.Scaled) > 0 {

					cover := card.Cover.Scaled[len(card.Cover.Scaled)-1]

					buf, err := migration.DownloadFile(cover.URL)
					if err != nil {
						return nil, err
					}

					coverAttachment := &models.TaskAttachment{
						ID: 43,
						File: &files.File{
							Name:        cover.ID + ".jpg",
							Mime:        "image/jpg", // Seems to always return jpg
							Size:        uint64(buf.Len()),
							FileContent: buf.Bytes(),
						},
					}

					task.Attachments = append(task.Attachments, coverAttachment)
					task.CoverImageAttachmentID = coverAttachment.ID
				}

				project.Tasks = append(project.Tasks, &models.TaskWithComments{Task: *task})
			}

			project.Buckets = append(project.Buckets, bucket)
			bucketID++
		}

		log.Debugf("[Trello Migration] Converted all cards to tasks for board %s", board.ID)

		fullVikunjaHierachie = append(fullVikunjaHierachie, project)
	}

	return
}

// Migrate gets all tasks from trello for a user and puts them into vikunja
// @Summary Migrate all projects, tasks etc. from trello
// @Description Migrates all projects, tasks, notes, reminders, subtasks and files from trello to vikunja.
// @tags migration
// @Accept json
// @Produce json
// @Security JWTKeyAuth
// @Param migrationCode body trello.Migration true "The auth token previously obtained from the auth url. See the docs for /migration/trello/auth."
// @Success 200 {object} models.Message "A message telling you everything was migrated successfully."
// @Failure 500 {object} models.Message "Internal server error"
// @Router /migration/trello/migrate [post]
func (m *Migration) Migrate(u *user.User) (err error) {
	log.Debugf("[Trello Migration] Starting migration for user %d", u.ID)
	log.Debugf("[Trello Migration] Getting all trello data for user %d", u.ID)

	trelloData, err := getTrelloData(m.Token)
	if err != nil {
		return
	}

	log.Debugf("[Trello Migration] Got all trello data for user %d", u.ID)
	log.Debugf("[Trello Migration] Start converting trello data for user %d", u.ID)

	fullVikunjaHierachie, err := convertTrelloDataToVikunja(trelloData, m.Token)
	if err != nil {
		return
	}

	log.Debugf("[Trello Migration] Done migrating trello data for user %d", u.ID)
	log.Debugf("[Trello Migration] Start inserting trello data for user %d", u.ID)

	err = migration.InsertFromStructure(fullVikunjaHierachie, u)
	if err != nil {
		return
	}

	log.Debugf("[Trello Migration] Done inserting trello data for user %d", u.ID)
	log.Debugf("[Trello Migration] Migration done for user %d", u.ID)

	return nil
}
