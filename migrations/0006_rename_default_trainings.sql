UPDATE trainings
SET title = 'Trening Goran Krstic',
    trainer = 'Goran Krstic'
WHERE title IN ('Morning HIIT', 'Trening with Goran Krstic');

UPDATE trainings
SET title = 'Trening Emanuela Krstic',
    trainer = 'fitnes instruktor Emanuela',
    description = 'Program za snagu, fleksibilnost i kontrolu pokreta.'
WHERE title IN ('Yoga Flow', 'Fitnes Emanuela Krstic', 'Trening Emanuela Krstic');
