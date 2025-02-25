<template>
	<ProjectWrapper
		class="project-list"
		:project-id="projectId"
		view-name="project"
	>
		<template #header>
			<div class="filter-container">
				<FilterPopup
					v-if="!isSavedFilter(project)"
					v-model="params"
					@update:modelValue="prepareFiltersAndLoadTasks()"
				/>
			</div>
		</template>

		<template #default>
			<div
				:class="{ 'is-loading': loading }"
				class="loader-container is-max-width-desktop list-view"
			>
				<card
					:padding="false"
					:has-content="false"
					class="has-overflow"
				>
					<AddTask
						v-if="!project.isArchived && canWrite"
						ref="addTaskRef"
						class="list-view__add-task d-print-none"
						:default-position="firstNewPosition"
						@taskAdded="updateTaskList"
					/>

					<Nothing v-if="ctaVisible && tasks.length === 0 && !loading">
						{{ $t('project.list.empty') }}
						<ButtonLink
							v-if="project.id > 0"
							@click="focusNewTaskInput()"
						>
							{{ $t('project.list.newTaskCta') }}
						</ButtonLink>
					</Nothing>


					<draggable
						v-if="tasks && tasks.length > 0"
						v-bind="DRAG_OPTIONS"
						v-model="tasks"
						group="tasks"
						handle=".handle"
						:disabled="!canWrite"
						item-key="id"
						tag="ul"
						:component-data="{
							class: {
								tasks: true,
								'dragging-disabled': !canWrite || isAlphabeticalSorting
							},
							type: 'transition-group'
						}"
						@start="() => drag = true"
						@end="saveTaskPosition"
					>
						<template #item="{element: t}">
							<SingleTaskInProject
								:show-list-color="false"
								:disabled="!canWrite"
								:can-mark-as-done="canWrite || isSavedFilter(project)"
								:the-task="t"
								:all-tasks="allTasks"
								@taskUpdated="updateTasks"
							>
								<template v-if="canWrite">
									<span class="icon handle">
										<icon icon="grip-lines" />
									</span>
								</template>
							</SingleTaskInProject>
						</template>
					</draggable>

					<Pagination
						:total-pages="totalPages"
						:current-page="currentPage"
					/>
				</card>
			</div>
		</template>
	</ProjectWrapper>
</template>

<script lang="ts">
export default {name: 'List'}
</script>

<script setup lang="ts">
import {ref, computed, nextTick, onMounted, watch} from 'vue'
import draggable from 'zhyswan-vuedraggable'

import ProjectWrapper from '@/components/project/ProjectWrapper.vue'
import ButtonLink from '@/components/misc/ButtonLink.vue'
import AddTask from '@/components/tasks/add-task.vue'
import SingleTaskInProject from '@/components/tasks/partials/singleTaskInProject.vue'
import FilterPopup from '@/components/project/partials/filter-popup.vue'
import Nothing from '@/components/misc/nothing.vue'
import Pagination from '@/components/misc/pagination.vue'
import {ALPHABETICAL_SORT} from '@/components/project/partials/filters.vue'

import {useTaskList} from '@/composables/useTaskList'
import {RIGHTS as Rights} from '@/constants/rights'
import {calculateItemPosition} from '@/helpers/calculateItemPosition'
import type {ITask} from '@/modelTypes/ITask'
import {isSavedFilter} from '@/services/savedFilter'

import {useBaseStore} from '@/stores/base'
import {useTaskStore} from '@/stores/tasks'

import type {IProject} from '@/modelTypes/IProject'

const {
	projectId,
} = defineProps<{
	projectId: IProject['id'],
}>()

const ctaVisible = ref(false)

const drag = ref(false)
const DRAG_OPTIONS = {
	animation: 100,
	ghostClass: 'task-ghost',
} as const

const {
	tasks: allTasks,
	loading,
	totalPages,
	currentPage,
	loadTasks,
	params,
	sortByParam,
} = useTaskList(() => projectId, {position: 'asc'})

const tasks = ref<ITask[]>([])
watch(
	allTasks,
	() => {
		tasks.value = [...allTasks.value]
		if (projectId < 0) {
			return
		}
		const tasksById = {}
		tasks.value.forEach(t => tasksById[t.id] = true)

		tasks.value = tasks.value.filter(t => {
			if (typeof t.relatedTasks?.parenttask === 'undefined') {
				return true
			}

			// If the task is a subtask, make sure the parent task is available in the current view as well
			for (const pt of t.relatedTasks.parenttask) {
				if (typeof tasksById[pt.id] === 'undefined') {
					return true
				}
			}

			return false
		})
	},
)

const isAlphabeticalSorting = computed(() => {
	return params.value.sort_by.find(sortBy => sortBy === ALPHABETICAL_SORT) !== undefined
})

const firstNewPosition = computed(() => {
	if (tasks.value.length === 0) {
		return 0
	}

	return calculateItemPosition(null, tasks.value[0].position)
})

const taskStore = useTaskStore()
const baseStore = useBaseStore()
const project = computed(() => baseStore.currentProject)

const canWrite = computed(() => {
	return project.value.maxRight > Rights.READ && project.value.id > 0
})

onMounted(async () => {
	await nextTick()
	ctaVisible.value = true
})

const addTaskRef = ref<typeof AddTask | null>(null)

function focusNewTaskInput() {
	addTaskRef.value?.focusTaskInput()
}

function updateTaskList(task: ITask) {
	if (isAlphabeticalSorting.value) {
		// reload tasks with current filter and sorting
		loadTasks()
	} else {
		allTasks.value = [
			task,
			...allTasks.value,
		]
	}

	baseStore.setHasTasks(true)
}

function updateTasks(updatedTask: ITask) {
	for (const t in tasks.value) {
		if (tasks.value[t].id === updatedTask.id) {
			tasks.value[t] = updatedTask
			break
		}
	}
}

async function saveTaskPosition(e) {
	drag.value = false

	const task = tasks.value[e.newIndex]
	const taskBefore = tasks.value[e.newIndex - 1] ?? null
	const taskAfter = tasks.value[e.newIndex + 1] ?? null

	const newTask = {
		...task,
		position: calculateItemPosition(taskBefore !== null ? taskBefore.position : null, taskAfter !== null ? taskAfter.position : null),
	}

	const updatedTask = await taskStore.update(newTask)
	tasks.value[e.newIndex] = updatedTask
}

function prepareFiltersAndLoadTasks() {
	if (isAlphabeticalSorting.value) {
		sortByParam.value = {}
		sortByParam.value[ALPHABETICAL_SORT] = 'asc'
	}

	loadTasks()
}
</script>

<style lang="scss" scoped>
.tasks {
	padding: .5rem;
}

.task-ghost {
	border-radius: $radius;
	background: var(--grey-100);
	border: 2px dashed var(--grey-300);

	* {
		opacity: 0;
	}
}

.list-view__add-task {
	padding: 1rem 1rem 0;
}

.link-share-view .card {
	border: none;
	box-shadow: none;
}

.control.has-icons-left .icon,
.control.has-icons-right .icon {
	transition: all $transition;
}

:deep(.single-task) {
	.handle {
		opacity: 1;
		transition: opacity $transition;
		margin-right: .25rem;
		cursor: grab;
	}

	@media(hover: hover) and (pointer: fine) {
		& .handle {
			opacity: 0;
		}

		&:hover .handle {
			opacity: 1;
		}
	}
}
</style>