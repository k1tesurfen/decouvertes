-- lua/plugins/decouvertes.lua
--
-- This is a LazyVim plugin that integrates the 'decouvertes' Go CLI.
-- This version adds full player management, persistent state, and stat display.

return {
	name = "decouvertes",
	dir = vim.fn.stdpath("config"),

	config = function()
		-- This table will hold the state of our game.
		local game_state = {
			is_active = false,
			win_id = nil,
			question_buf_id = nil,
			answer_buf_id = nil,
			current_player_id = nil,
			current_player_name = nil,
			current_card = nil, -- Store the full card object
			stats_visible = false, -- Track if stats are being shown
		}

		-- Forward declarations for functions that call each other.
		local draw_next_card
		local stop_game
		local start_game
		local handle_player_selection
		local draw_question

		-- --- Player and Session Management ---

		local session_file = vim.fn.stdpath("config") .. "/decouvertes/session.json"

		local function save_current_player()
			if game_state.current_player_id then
				local file = io.open(session_file, "w")
				if file then
					file:write(vim.json.encode({ current_player_id = game_state.current_player_id }))
					file:close()
				end
			end
		end

		local function load_current_player()
			local file = io.open(session_file, "r")
			if file then
				local content = file:read("*a")
				file:close()
				local ok, data = pcall(vim.json.decode, content)
				if ok and data.current_player_id then
					game_state.current_player_id = data.current_player_id
					return true
				end
			end
			return false
		end

		-- --- Core Game Logic ---

		local function handle_answer()
			local answer = vim.api.nvim_buf_get_lines(game_state.answer_buf_id, 0, -1, false)[1] or ""
			vim.api.nvim_buf_set_lines(game_state.answer_buf_id, 0, -1, false, { "" })
			vim.api.nvim_set_current_win(game_state.win_id)
			vim.cmd("stopinsert")

			if not game_state.current_card then
				return
			end

			vim.system({
				"decouvertes",
				"check-answer",
				"--player-id=" .. game_state.current_player_id,
				"--id=" .. game_state.current_card.id,
				"--answer=" .. answer,
			}, { text = true }, function(check_result)
				vim.schedule(function()
					if check_result.code ~= 0 then
						vim.notify("Decouvertes CLI Error on check:\n" .. check_result.stderr, vim.log.levels.ERROR)
						return
					end
					local check_ok, res = pcall(vim.json.decode, vim.trim(check_result.stdout))
					if not check_ok then
						vim.notify("Failed to parse JSON result: " .. tostring(res), vim.log.levels.ERROR)
						return
					end
					if res.correct then
						vim.notify("✅ Correct! Card moved to box " .. res.new_box, vim.log.levels.INFO)
					else
						vim.notify("❌ Incorrect. The correct answer was:\n" .. res.solution, vim.log.levels.WARN)
					end
					draw_next_card()
				end)
			end)
		end

		-- Draws the provided card object into the question buffer.
		draw_question = function(card)
			if not card then
				return
			end
			local lines = {
				"Language: " .. card.language,
				"",
				"Question:",
				"------------------",
			}
			for s in card.prompt:gmatch("[^\r\n]+") do
				table.insert(lines, s)
			end
			vim.api.nvim_buf_set_option(game_state.question_buf_id, "modifiable", true)
			vim.api.nvim_buf_set_lines(game_state.question_buf_id, 0, -1, false, lines)
			vim.api.nvim_buf_set_option(game_state.question_buf_id, "modifiable", false)
		end

		draw_next_card = function()
			if not game_state.is_active then
				return
			end
			vim.system(
				{ "decouvertes", "get-card", "--player-id=" .. game_state.current_player_id },
				{ text = true },
				function(result)
					vim.schedule(function()
						if result.code ~= 0 then
							vim.notify("Decouvertes CLI Error:\n" .. result.stderr, vim.log.levels.ERROR)
							stop_game()
							return
						end

						local ok, card = pcall(vim.json.decode, vim.trim(result.stdout))
						if not ok then
							vim.notify(
								"Failed to parse JSON from decouvertes CLI: " .. tostring(card),
								vim.log.levels.ERROR
							)
							stop_game()
							return
						end

						if card.id == "done" then
							vim.notify(card.prompt, vim.log.levels.INFO)
							stop_game()
							return
						end

						game_state.current_card = card
						game_state.stats_visible = false
						draw_question(game_state.current_card)
					end)
				end
			)
		end

		local function toggle_stats_display()
			if not game_state.current_player_id then
				vim.notify("No active player selected.", vim.log.levels.WARN)
				return
			end

			-- If stats are visible, hide them and show the card again.
			if game_state.stats_visible then
				draw_question(game_state.current_card)
				game_state.stats_visible = false
				return
			end

			-- Otherwise, fetch and show the stats.
			vim.system(
				{ "decouvertes", "get-stats", "--player-id=" .. game_state.current_player_id },
				{ text = true },
				function(result)
					vim.schedule(function()
						if result.code ~= 0 then
							vim.notify("Could not get stats:\n" .. result.stderr, vim.log.levels.ERROR)
							return
						end
						local lines = {}
						for s in result.stdout:gmatch("[^\r\n]+") do
							table.insert(lines, s)
						end
						vim.api.nvim_buf_set_option(game_state.question_buf_id, "modifiable", true)
						vim.api.nvim_buf_set_lines(game_state.question_buf_id, 0, -1, false, lines)
						vim.api.nvim_buf_set_option(game_state.question_buf_id, "modifiable", false)
						game_state.stats_visible = true
					end)
				end
			)
		end

		-- --- UI and Flow Control ---

		local function handle_create_player(callback)
			vim.ui.input({ prompt = "Enter new player name: " }, function(name)
				if not name or name == "" then
					vim.notify("Player creation cancelled.", vim.log.levels.WARN)
					return
				end
				vim.system({ "decouvertes", "create-player", "--name=" .. name }, { text = true }, function(result)
					vim.schedule(function()
						if result.code ~= 0 then
							vim.notify("Failed to create player:\n" .. result.stderr, vim.log.levels.ERROR)
							return
						end
						game_state.current_player_id = vim.trim(result.stdout)
						game_state.current_player_name = name
						save_current_player()
						vim.notify("Player '" .. name .. "' created and selected.", vim.log.levels.INFO)
						if callback then
							callback()
						end
					end)
				end)
			end)
		end

		handle_player_selection = function(callback)
			vim.system({ "decouvertes", "list-players" }, { text = true }, function(result)
				vim.schedule(function()
					local players = {}
					local player_map = {}
					for line in result.stdout:gmatch("[^\r\n]+") do
						local name, id = line:match("Name: (.+), ID: (.+)")
						if name and id then
							table.insert(players, name)
							player_map[name] = id
						end
					end

					table.insert(players, "[ Create New Player ]")

					vim.ui.select(players, { prompt = "Select a player:" }, function(choice)
						if not choice then
							vim.notify("Player selection cancelled.", vim.log.levels.WARN)
							return
						end
						if choice == "[ Create New Player ]" then
							handle_create_player(callback)
						else
							game_state.current_player_id = player_map[choice]
							game_state.current_player_name = choice
							save_current_player()
							vim.notify("Player '" .. choice .. "' selected.", vim.log.levels.INFO)
							if callback then
								callback()
							end
						end
					end)
				end)
			end)
		end

		start_game = function()
			if game_state.is_active then
				return
			end
			if not game_state.current_player_id then
				vim.notify("No player selected. Opening player selection...", vim.log.levels.INFO)
				handle_player_selection(start_game) -- Start game after player is selected/created
				return
			end

			game_state.is_active = true
			game_state.question_buf_id = vim.api.nvim_create_buf(false, true)
			game_state.answer_buf_id = vim.api.nvim_create_buf(false, true)
			vim.api.nvim_buf_set_lines(game_state.answer_buf_id, 0, -1, false, { "" })
			vim.api.nvim_buf_set_option(game_state.question_buf_id, "modifiable", false)
			vim.api.nvim_buf_set_option(game_state.answer_buf_id, "filetype", "decouvertes-answer")

			local width = math.floor(vim.o.columns * 0.8)
			local height = math.floor(vim.o.lines * 0.6)
			game_state.win_id = vim.api.nvim_open_win(game_state.question_buf_id, true, {
				relative = "editor",
				width = width,
				height = height,
				row = math.floor((vim.o.lines - height) / 2),
				col = math.floor((vim.o.columns - width) / 2),
				border = "rounded",
				title = "decouvertes | "
					.. (game_state.current_player_name or "Player")
					.. " | [A]nswer [S]tats [P]layer [Q]uit",
				focusable = false,
				style = "minimal",
			})

			game_state.answer_win_id = vim.api.nvim_open_win(game_state.answer_buf_id, false, {
				relative = "win",
				win = game_state.win_id,
				width = width - 2,
				height = 1,
				row = height - 2,
				col = 1,
				style = "minimal",
				border = "single",
				title = "Answer",
				title_pos = "left",
			})

			vim.api.nvim_buf_set_keymap(
				game_state.question_buf_id,
				"n",
				"q",
				"<cmd>lua require('plugins.decouvertes').stop_game()<CR>",
				{ noremap = true, silent = true }
			)
			vim.api.nvim_buf_set_keymap(
				game_state.question_buf_id,
				"n",
				"a",
				"<cmd>lua require('plugins.decouvertes').focus_answer()<CR>",
				{ noremap = true, silent = true }
			)
			vim.api.nvim_buf_set_keymap(
				game_state.question_buf_id,
				"n",
				"s",
				"<cmd>lua require('plugins.decouvertes').toggle_stats_display()<CR>",
				{ noremap = true, silent = true }
			)
			vim.api.nvim_buf_set_keymap(
				game_state.question_buf_id,
				"n",
				"p",
				"<cmd>lua require('plugins.decouvertes').handle_player_selection(nil)<CR>",
				{ noremap = true, silent = true }
			)
			vim.api.nvim_buf_set_keymap(
				game_state.answer_buf_id,
				"i",
				"<CR>",
				"<cmd>lua require('plugins.decouvertes').handle_answer()<CR>",
				{ noremap = true, silent = true }
			)

			draw_next_card()
		end

		stop_game = function()
			if not game_state.is_active then
				return
			end
			if vim.api.nvim_win_is_valid(game_state.win_id) then
				vim.api.nvim_win_close(game_state.win_id, true)
			end
			if vim.api.nvim_win_is_valid(game_state.answer_win_id) then
				vim.api.nvim_win_close(game_state.answer_win_id, true)
			end
			game_state.is_active = false
		end

		local function focus_answer()
			vim.api.nvim_set_current_win(game_state.answer_win_id)
			vim.cmd("startinsert")
		end

		-- Expose functions globally for keymaps
		_G.require("plugins.decouvertes").stop_game = stop_game
		_G.require("plugins.decouvertes").focus_answer = focus_answer
		_G.require("plugins.decouvertes").handle_answer = handle_answer
		_G.require("plugins.decouvertes").toggle_stats_display = toggle_stats_display
		_G.require("plugins.decouvertes").handle_player_selection = handle_player_selection

		-- Load the last active player on startup
		load_current_player()

		-- Create user commands and keymaps
		vim.api.nvim_create_user_command("Decouvertes", start_game, { desc = "Start a decouvertes review session" })
		vim.api.nvim_create_user_command("DecouvertesPlayer", function()
			handle_player_selection(nil)
		end, { desc = "Select or create a decouvertes player" })

		vim.keymap.set(
			"n",
			"<leader>dv",
			start_game,
			{ noremap = true, silent = true, desc = "decouvertes: Start review" }
		)
		vim.keymap.set("n", "<leader>dp", function()
			handle_player_selection(nil)
		end, { noremap = true, silent = true, desc = "decouvertes: Player menu" })
	end,
}
