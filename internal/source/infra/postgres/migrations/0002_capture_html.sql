alter table source.capture drop constraint if exists capture_media_type_check;
alter table source.capture add constraint capture_media_type_check check (media_type in ('text/plain','text/html','application/json','application/pdf','image/png','image/jpeg','image/webp'));
alter table source.capture drop constraint if exists capture_bytes_check;
alter table source.capture add constraint capture_bytes_check check (bytes between 1 and 8388608);
