-- name: GetScale :one
SELECT code, name, active, sort_order
FROM garage.scale
WHERE code = $1;

-- name: GetPowertrain :one
SELECT code, name, family::text AS family, active, sort_order
FROM garage.powertrain
WHERE code = $1;

-- name: ListScales :many
SELECT code, name, active, sort_order
FROM garage.scale
ORDER BY code;

-- name: ListPowertrains :many
SELECT code, name, family::text AS family, active, sort_order
FROM garage.powertrain
ORDER BY code;
