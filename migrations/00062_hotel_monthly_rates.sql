-- +goose Up
CREATE TABLE hotel_monthly_rates (
    hotel_id  UUID    NOT NULL REFERENCES hotels(id) ON DELETE CASCADE,
    year      INT     NOT NULL,
    month     INT     NOT NULL CHECK (month BETWEEN 1 AND 12),
    room_type TEXT    NOT NULL,
    net       BIGINT  NOT NULL DEFAULT 0 CHECK (net >= 0),
    sell      BIGINT  NOT NULL DEFAULT 0 CHECK (sell >= 0),
    PRIMARY KEY (hotel_id, year, month, room_type)
);

CREATE INDEX hotel_monthly_rates_hotel_year_idx ON hotel_monthly_rates (hotel_id, year);

-- +goose Down
DROP TABLE IF EXISTS hotel_monthly_rates;
