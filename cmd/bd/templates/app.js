document.addEventListener('DOMContentLoaded', function() {
    const searchInput = document.getElementById('search');
    const statusFilter = document.getElementById('status-filter');
    const priorityFilter = document.getElementById('priority-filter');
    const rows = document.querySelectorAll('.issue-row');

    function filterIssues() {
        const searchTerm = searchInput.value.toLowerCase();
        const statusValue = statusFilter.value;
        const priorityValue = priorityFilter.value;

        rows.forEach(row => {
            const title = row.querySelector('.title').textContent.toLowerCase();
            const id = row.cells[0].textContent.toLowerCase();
            const matchesSearch = title.includes(searchTerm) || id.includes(searchTerm);
            
            const matchesStatus = !statusValue || row.classList.contains('status-' + statusValue);
            const matchesPriority = !priorityValue || row.classList.contains('priority-' + priorityValue);

            if (matchesSearch && matchesStatus && matchesPriority) {
                row.classList.remove('hidden');
            } else {
                row.classList.add('hidden');
            }
        });
    }

    searchInput.addEventListener('input', filterIssues);
    statusFilter.addEventListener('change', filterIssues);
    priorityFilter.addEventListener('change', filterIssues);
});
