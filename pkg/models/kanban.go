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

package models

import (
	"strconv"
	"strings"
	"time"

	"code.vikunja.io/api/pkg/log"
	"code.vikunja.io/api/pkg/user"
	"code.vikunja.io/web"
	"xorm.io/xorm"
)

// Bucket represents a kanban bucket
type Bucket struct {
	// The unique, numeric id of this bucket.
	ID int64 `xorm:"bigint autoincr not null unique pk" json:"id" param:"bucket"`
	// The title of this bucket.
	Title string `xorm:"text not null" valid:"required" minLength:"1" json:"title"`
	// The project this bucket belongs to.
	ProjectID int64 `xorm:"bigint not null" json:"project_id" param:"project"`
	// All tasks which belong to this bucket.
	Tasks []*Task `xorm:"-" json:"tasks"`

	// How many tasks can be at the same time on this board max
	Limit int64 `xorm:"default 0" json:"limit" minimum:"0" valid:"range(0|9223372036854775807)"`

	// The number of tasks currently in this bucket
	Count int64 `xorm:"-" json:"count"`

	// The position this bucket has when querying all buckets. See the tasks.position property on how to use this.
	Position float64 `xorm:"double null" json:"position"`

	// A timestamp when this bucket was created. You cannot change this value.
	Created time.Time `xorm:"created not null" json:"created"`
	// A timestamp when this bucket was last updated. You cannot change this value.
	Updated time.Time `xorm:"updated not null" json:"updated"`

	// The user who initially created the bucket.
	CreatedBy   *user.User `xorm:"-" json:"created_by" valid:"-"`
	CreatedByID int64      `xorm:"bigint not null" json:"-"`

	// Including the task collection type so we can use task filters on kanban
	TaskCollection `xorm:"-" json:"-"`

	web.Rights   `xorm:"-" json:"-"`
	web.CRUDable `xorm:"-" json:"-"`
}

// TableName returns the table name for this bucket.
func (b *Bucket) TableName() string {
	return "buckets"
}

func getBucketByID(s *xorm.Session, id int64) (b *Bucket, err error) {
	b = &Bucket{}
	exists, err := s.Where("id = ?", id).Get(b)
	if err != nil {
		return
	}
	if !exists {
		return b, ErrBucketDoesNotExist{BucketID: id}
	}
	return
}

func getDefaultBucketID(s *xorm.Session, project *Project) (bucketID int64, err error) {
	if project.DefaultBucketID != 0 {
		return project.DefaultBucketID, nil
	}

	bucket := &Bucket{}
	_, err = s.
		Where("project_id = ?", project.ID).
		OrderBy("position asc").
		Get(bucket)
	if err != nil {
		return 0, err
	}

	return bucket.ID, nil
}

// ReadAll returns all buckets with their tasks for a certain project
// @Summary Get all kanban buckets of a project
// @Description Returns all kanban buckets with belong to a project including their tasks. Buckets are always sorted by their `position` in ascending order. Tasks are sorted by their `kanban_position` in ascending order.
// @tags project
// @Accept json
// @Produce json
// @Security JWTKeyAuth
// @Param id path int true "Project Id"
// @Param page query int false "The page number for tasks. Used for pagination. If not provided, the first page of results is returned."
// @Param per_page query int false "The maximum number of tasks per bucket per page. This parameter is limited by the configured maximum of items per page."
// @Param s query string false "Search tasks by task text."
// @Param filter query string false "The filter query to match tasks by. Check out https://vikunja.io/docs/filters for a full explanation of the feature."
// @Param filter_timezone query string false "The time zone which should be used for date match (statements like "now" resolve to different actual times)"
// @Param filter_include_nulls query string false "If set to true the result will include filtered fields whose value is set to `null`. Available values are `true` or `false`. Defaults to `false`."
// @Success 200 {array} models.Bucket "The buckets with their tasks"
// @Failure 500 {object} models.Message "Internal server error"
// @Router /projects/{id}/buckets [get]
func (b *Bucket) ReadAll(s *xorm.Session, auth web.Auth, search string, page int, perPage int) (result interface{}, resultCount int, numberOfTotalItems int64, err error) {

	project, err := GetProjectSimpleByID(s, b.ProjectID)
	if err != nil {
		return nil, 0, 0, err
	}

	can, _, err := project.CanRead(s, auth)
	if err != nil {
		return nil, 0, 0, err
	}
	if !can {
		return nil, 0, 0, ErrGenericForbidden{}
	}

	// Get all buckets for this project
	buckets := []*Bucket{}
	err = s.
		Where("project_id = ?", b.ProjectID).
		OrderBy("position").
		Find(&buckets)
	if err != nil {
		return
	}

	// Make a map from the bucket slice with their id as key so that we can use it to put the tasks in their buckets
	bucketMap := make(map[int64]*Bucket, len(buckets))
	userIDs := make([]int64, 0, len(buckets))
	for _, bb := range buckets {
		bucketMap[bb.ID] = bb
		userIDs = append(userIDs, bb.CreatedByID)
	}

	// Get all users
	users, err := getUsersOrLinkSharesFromIDs(s, userIDs)
	if err != nil {
		return
	}

	for _, bb := range buckets {
		bb.CreatedBy = users[bb.CreatedByID]
	}

	tasks := []*Task{}

	opts, err := getTaskFilterOptsFromCollection(&b.TaskCollection)
	if err != nil {
		return nil, 0, 0, err
	}

	opts.sortby = []*sortParam{
		{
			orderBy: orderAscending,
			sortBy:  taskPropertyKanbanPosition,
		},
	}
	opts.page = page
	opts.perPage = perPage
	opts.search = search

	for _, filter := range opts.parsedFilters {
		if filter.field == taskPropertyBucketID {

			// Limiting the map to the one filter we're looking for is the easiest way to ensure we only
			// get tasks in this bucket
			bucketID := filter.value.(int64)
			bucket := bucketMap[bucketID]

			bucketMap = make(map[int64]*Bucket, 1)
			bucketMap[bucketID] = bucket
			break
		}
	}

	originalFilter := opts.filter
	for id, bucket := range bucketMap {

		if !strings.Contains(originalFilter, "bucket_id") {
			var filterString string
			if originalFilter == "" {
				filterString = "bucket_id = " + strconv.FormatInt(id, 10)
			} else {
				filterString = "(" + originalFilter + ") && bucket_id = " + strconv.FormatInt(id, 10)
			}
			opts.parsedFilters, err = getTaskFiltersFromFilterString(filterString, opts.filterTimezone)
			if err != nil {
				return
			}
		}

		ts, _, total, err := getRawTasksForProjects(s, []*Project{{ID: bucket.ProjectID}}, auth, opts)
		if err != nil {
			return nil, 0, 0, err
		}

		bucket.Count = total

		tasks = append(tasks, ts...)
	}

	taskMap := make(map[int64]*Task, len(tasks))
	for _, t := range tasks {
		taskMap[t.ID] = t
	}

	err = addMoreInfoToTasks(s, taskMap, auth)
	if err != nil {
		return nil, 0, 0, err
	}

	// Put all tasks in their buckets
	// All tasks which are not associated to any bucket will have bucket id 0 which is the nil value for int64
	// Since we created a bucked with that id at the beginning, all tasks should be in there.
	for _, task := range tasks {
		// Check if the bucket exists in the map to prevent nil pointer panics
		if _, exists := bucketMap[task.BucketID]; !exists {
			log.Debugf("Tried to put task %d into bucket %d which does not exist in project %d", task.ID, task.BucketID, b.ProjectID)
			continue
		}
		bucketMap[task.BucketID].Tasks = append(bucketMap[task.BucketID].Tasks, task)
	}

	return buckets, len(buckets), int64(len(buckets)), nil
}

// Create creates a new bucket
// @Summary Create a new bucket
// @Description Creates a new kanban bucket on a project.
// @tags project
// @Accept json
// @Produce json
// @Security JWTKeyAuth
// @Param id path int true "Project Id"
// @Param bucket body models.Bucket true "The bucket object"
// @Success 200 {object} models.Bucket "The created bucket object."
// @Failure 400 {object} web.HTTPError "Invalid bucket object provided."
// @Failure 404 {object} web.HTTPError "The project does not exist."
// @Failure 500 {object} models.Message "Internal error"
// @Router /projects/{id}/buckets [put]
func (b *Bucket) Create(s *xorm.Session, a web.Auth) (err error) {
	b.CreatedBy, err = GetUserOrLinkShareUser(s, a)
	if err != nil {
		return
	}
	b.CreatedByID = b.CreatedBy.ID

	_, err = s.Insert(b)
	if err != nil {
		return
	}

	b.Position = calculateDefaultPosition(b.ID, b.Position)
	_, err = s.Where("id = ?", b.ID).Update(b)
	return
}

// Update Updates an existing bucket
// @Summary Update an existing bucket
// @Description Updates an existing kanban bucket.
// @tags project
// @Accept json
// @Produce json
// @Security JWTKeyAuth
// @Param projectID path int true "Project Id"
// @Param bucketID path int true "Bucket Id"
// @Param bucket body models.Bucket true "The bucket object"
// @Success 200 {object} models.Bucket "The created bucket object."
// @Failure 400 {object} web.HTTPError "Invalid bucket object provided."
// @Failure 404 {object} web.HTTPError "The bucket does not exist."
// @Failure 500 {object} models.Message "Internal error"
// @Router /projects/{projectID}/buckets/{bucketID} [post]
func (b *Bucket) Update(s *xorm.Session, _ web.Auth) (err error) {
	_, err = s.
		Where("id = ?", b.ID).
		Cols(
			"title",
			"limit",
			"position",
		).
		Update(b)
	return
}

// Delete removes a bucket, but no tasks
// @Summary Deletes an existing bucket
// @Description Deletes an existing kanban bucket and dissociates all of its task. It does not delete any tasks. You cannot delete the last bucket on a project.
// @tags project
// @Accept json
// @Produce json
// @Security JWTKeyAuth
// @Param projectID path int true "Project Id"
// @Param bucketID path int true "Bucket Id"
// @Success 200 {object} models.Message "Successfully deleted."
// @Failure 404 {object} web.HTTPError "The bucket does not exist."
// @Failure 500 {object} models.Message "Internal error"
// @Router /projects/{projectID}/buckets/{bucketID} [delete]
func (b *Bucket) Delete(s *xorm.Session, a web.Auth) (err error) {

	// Prevent removing the last bucket
	total, err := s.Where("project_id = ?", b.ProjectID).Count(&Bucket{})
	if err != nil {
		return
	}
	if total <= 1 {
		return ErrCannotRemoveLastBucket{
			BucketID:  b.ID,
			ProjectID: b.ProjectID,
		}
	}

	// Get the default bucket
	p, err := GetProjectSimpleByID(s, b.ProjectID)
	if err != nil {
		return
	}
	var updateProject bool
	if b.ID == p.DefaultBucketID {
		p.DefaultBucketID = 0
		updateProject = true
	}
	if b.ID == p.DoneBucketID {
		p.DoneBucketID = 0
		updateProject = true
	}
	if updateProject {
		err = p.Update(s, a)
		if err != nil {
			return
		}
	}

	defaultBucketID, err := getDefaultBucketID(s, p)
	if err != nil {
		return err
	}

	// Remove all associations of tasks to that bucket
	_, err = s.
		Where("bucket_id = ?", b.ID).
		Cols("bucket_id").
		Update(&Task{BucketID: defaultBucketID})
	if err != nil {
		return
	}

	// Remove the bucket itself
	_, err = s.Where("id = ?", b.ID).Delete(&Bucket{})
	return
}
