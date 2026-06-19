-- Kiosk module (increment 4): idle video screensaver support.

ALTER TABLE kiosk_settings
    ADD COLUMN idle_video_url VARCHAR(500) NULL DEFAULT NULL AFTER idle_image_url;
