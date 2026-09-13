ALTER TABLE `todo` ADD `id` text;
--> statement-breakpoint
ALTER TABLE `todo` ADD `plan_id` text;
--> statement-breakpoint
UPDATE `todo`
SET `id` = 'todo_' || `session_id` || '_' || `position` || '_' || `time_created`
WHERE `id` IS NULL;
--> statement-breakpoint
UPDATE `todo`
SET `plan_id` = 'plan_' || `session_id` || '_' || `time_created`
WHERE `plan_id` IS NULL;
--> statement-breakpoint
CREATE TABLE `__new_todo` (
	`id` text PRIMARY KEY NOT NULL,
	`session_id` text NOT NULL,
	`plan_id` text NOT NULL,
	`content` text NOT NULL,
	`status` text NOT NULL,
	`priority` text NOT NULL,
	`position` integer NOT NULL,
	`time_created` integer NOT NULL,
	`time_updated` integer NOT NULL,
	CONSTRAINT `fk_todo_session_id_session_id_fk` FOREIGN KEY (`session_id`) REFERENCES `session`(`id`) ON DELETE CASCADE
);
--> statement-breakpoint
INSERT INTO `__new_todo` (`id`, `session_id`, `plan_id`, `content`, `status`, `priority`, `position`, `time_created`, `time_updated`)
SELECT `id`, `session_id`, `plan_id`, `content`, `status`, `priority`, `position`, `time_created`, `time_updated`
FROM `todo`;
--> statement-breakpoint
DROP TABLE `todo`;
--> statement-breakpoint
ALTER TABLE `__new_todo` RENAME TO `todo`;
--> statement-breakpoint
CREATE INDEX `todo_session_idx` ON `todo` (`session_id`);
--> statement-breakpoint
CREATE INDEX `todo_session_plan_idx` ON `todo` (`session_id`, `plan_id`);
