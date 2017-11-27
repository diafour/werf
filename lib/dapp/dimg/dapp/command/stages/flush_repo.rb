module Dapp
  module Dimg
    module Dapp
      module Command
        module Stages
          module FlushRepo
            def stages_flush_repo
              lock_repo(option_repo) do
                log_step_with_indent(option_repo) do
                  registry    = registry(option_repo)
                  repo_dimgs  = repo_dimgs_images(registry)
                  repo_dimgstages = repo_dimgstages_images(registry)

                  repo_dimgs.concat(repo_dimgstages).each { |repo_image| delete_repo_image(registry, repo_image) }
                end
              end
            end
          end
        end
      end
    end
  end # Dimg
end # Dapp
