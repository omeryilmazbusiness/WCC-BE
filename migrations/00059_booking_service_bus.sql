-- +goose Up
-- Road-coach bookings sit next to flight in the service-type picker.
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_service_type_check;
ALTER TABLE bookings ADD CONSTRAINT bookings_service_type_check
    CHECK (service_type IN ('flight','bus','hotel','package','transfer','visa','tour'));

-- +goose Down
UPDATE bookings SET service_type = 'transfer' WHERE service_type = 'bus';
ALTER TABLE bookings DROP CONSTRAINT IF EXISTS bookings_service_type_check;
ALTER TABLE bookings ADD CONSTRAINT bookings_service_type_check
    CHECK (service_type IN ('flight','hotel','package','transfer','visa','tour'));
