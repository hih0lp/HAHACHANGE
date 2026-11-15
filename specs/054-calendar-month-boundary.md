# SDD-054: Moscow calendar month boundary

For calendar questions, the API uses the current calendar month in `Europe/Moscow`, from 00:00 on its first day to 00:00 on the first day of the next month. This replaces the rolling seven-day mentor window and rolling 30-day company window. The current month is selected for an unspecified period, including a follow-up in a chat with recent messages. The planner still uses recent chat to resolve references such as “завтра он свободен?”.

All calendar MCP calls are limited to the same month. A requested range crossing the boundary is intersected with the month; a range entirely outside the month is rejected without calling the calendar. This applies both to server-planned calls and to model-selected tools. Date comparisons use absolute instants while boundary labels are formatted in Moscow time. Answers state the checked interval and cannot infer availability beyond it.

The `/v1/calendar/events` route lists only the current month. The next-day reminder route clips its range at the same month boundary.

Verify default month, crossed-boundary clipping, outside-month rejection, and a follow-up with recent chat. The test stack must answer 16 November using only the calendar's 16 November data, without turning UTC timestamps into Moscow clock times.
