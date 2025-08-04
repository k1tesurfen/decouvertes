-- lua/plugins/decouvertes.lua
--
-- This is a LazyVim plugin that integrates the 'decouvertes' Go CLI.
-- It provides a user command and a keymap to start a review session.
-- This version features a persistent game loop with key-based controls.

return {
	name = "decouvertes",
	dir = vim.fn.stdpath("config"),

	config = function()
		-- This table will hold the state of our game, like window and buffer IDs.
		local game_state = {
			is_active = false,
			win_id = nil,
			question_buf_id = nil,
			answer_buf_id = nil,
		}

		-- Forward declaration for functions that call each other.
		local draw_next_card
		local stop_game

		-- This function handles the answer submission.
		local function handle_answer()
			-- Get the user's answer from the answer buffer.
			local answer = vim.api.nvim_buf_get_lines(game_state.answer_buf_id, 0, -1, false)[1] or ""

			-- Clear the answer buffer for the next round.
			vim.api.nvim_buf_set_lines(game_state.answer_buf_id, 0, -1, false, { "" })

			-- Switch back to the question window and exit insert mode.
			vim.api.nvim_set_current_win(game_state.win_id)
			vim.cmd("stopinsert") -- <-- Automatically exit insert mode.

			if not game_state.current_card_id then
				return
			end

			-- Call the CLI to check the answer.
			vim.system(
				{ "decouvertes", "check-answer", "--id=" .. game_state.current_card_id, "--answer=" .. answer },
				{ text = true },
				function(check_result)
					vim.schedule(function()
						if check_result.code ~= 0 then
							vim.notify("Decouvertes CLI Error on check:\n" .. check_result.stderr, vim.log.levels.ERROR)
							stop_game()
							return
						end

						local check_ok, res = pcall(vim.json.decode, vim.trim(check_result.stdout))
						if not check_ok then
							vim.notify("Failed to parse JSON result: " .. tostring(res), vim.log.levels.ERROR)
							return
						end

						-- Notify the user and draw the next card.
						if res.correct then
							vim.notify("✅ Correct! Card moved to box " .. res.new_box, vim.log.levels.INFO)
						else
							vim.notify("❌ Incorrect. The correct answer was:\n" .. res.solution, vim.log.levels.WARN)
						end
						draw_next_card()
					end)
				end
			)
		end

		-- This function fetches a new card and draws it in the question buffer.
		draw_next_card = function()
			if not game_state.is_active then
				return
			end

			vim.system({ "decouvertes", "get-card" }, { text = true }, function(result)
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

					-- Store the current card ID for the answer check.
					game_state.current_card_id = card.id

					-- Prepare the content for the question window.
					local lines = {
						"Language: " .. card.language,
						"",
						"Question:",
						"------------------",
					}
					for s in card.prompt:gmatch("[^\r\n]+") do
						table.insert(lines, s)
					end

					-- Update the question buffer with the new card.
					vim.api.nvim_buf_set_option(game_state.question_buf_id, "modifiable", true)
					vim.api.nvim_buf_set_lines(game_state.question_buf_id, 0, -1, false, lines)
					vim.api.nvim_buf_set_option(game_state.question_buf_id, "modifiable", false)
				end)
			end)
		end

		-- This function creates all the UI and starts the game loop.
		local function start_game()
			if game_state.is_active then
				return
			end
			game_state.is_active = true

			-- Create buffers for the question and the answer input.
			game_state.question_buf_id = vim.api.nvim_create_buf(false, true)
			game_state.answer_buf_id = vim.api.nvim_create_buf(false, true)
			vim.api.nvim_buf_set_lines(game_state.answer_buf_id, 0, -1, false, { "" })

			-- Set options for the buffers.
			vim.api.nvim_buf_set_option(game_state.question_buf_id, "modifiable", false)
			vim.api.nvim_buf_set_option(game_state.answer_buf_id, "filetype", "decouvertes-answer")

			-- Create the main floating window.
			local width = math.floor(vim.o.columns * 0.8)
			local height = math.floor(vim.o.lines * 0.6)
			game_state.win_id = vim.api.nvim_open_win(game_state.question_buf_id, true, {
				relative = "editor",
				width = width,
				height = height,
				row = math.floor((vim.o.lines - height) / 2),
				col = math.floor((vim.o.columns - width) / 2),
				border = "rounded",
				title = "Découvertes",
				focusable = false, -- <-- Make the window non-focusable.
				style = "minimal",
			})

			-- Create a small window at the bottom for the answer.
			local answer_win_height = 3
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

			-- Set the status line with instructions.
			vim.api.nvim_win_set_option(game_state.win_id, "statusline", "[A]nswer | [Q]uit")

			-- Set keymaps for the game window.
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

			-- Set keymap for submitting the answer from the answer buffer.
			vim.api.nvim_buf_set_keymap(
				game_state.answer_buf_id,
				"i",
				"<CR>",
				"<cmd>lua require('plugins.decouvertes').handle_answer()<CR>",
				{ noremap = true, silent = true }
			)

			-- Kick off the game by drawing the first card.
			draw_next_card()
		end

		-- This function cleans up the UI and resets the state.
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
			-- Reset the state for the next game.
			game_state = { is_active = false }
		end

		-- Helper function to focus the answer window.
		local function focus_answer()
			vim.api.nvim_set_current_win(game_state.answer_win_id)
			vim.cmd("startinsert")
		end

		-- We need to expose some functions globally so the keymaps can call them.
		-- This is a common pattern for Neovim plugin callbacks.
		_G.require("plugins.decouvertes").stop_game = stop_game
		_G.require("plugins.decouvertes").focus_answer = focus_answer
		_G.require("plugins.decouvertes").handle_answer = handle_answer

		-- Create a user command so you can type :Decouvertes to start.
		vim.api.nvim_create_user_command("Decouvertes", start_game, {
			desc = "Start a Découvertes review session",
		})

		-- Create a keymap. <leader>dv (for Découvertes) is the new keymap.
		vim.keymap.set("n", "<leader>dv", start_game, {
			noremap = true,
			silent = true,
			desc = "Decouvertes: Start review",
		})
	end,
}
